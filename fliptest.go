// Package fliptest provides a mechanism for testing internet egress
// in an AWS VPC by creating a VPC Lambda via Cloudformation stack
// to which custom test URLs can be passed.
package fliptest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/lambda"
)

// Unless overridden using FlipTesterInput the stack will
// be prefixed with this and a random number will be added to the end.
const DefaultStackPrefix string = "ISS-GR-egress-tester-"

// FlipTesterInput provides all of the information necessary
// to create a FlipTester object.
type FlipTesterInput struct {

	// The SubnetId in which to launch the test lambda.
	// The egress path that is provided by this subnet's
	// route table and NACLs will determine whether or not
	// the egress test is successful.
	SubnetId string

	// The VpcId in which to launch the test lambda.
	VpcId string

	// A prefix to give to the created Cloudformation
	// stack. If none is provided then a default of
	// defaultStackPrefix const will be used.
	StackPrefix string

	// If a custom Cloudformation template is desired
	// then a filename can be provided here and the
	// FlipTester will attempt to load it and create
	// the stack using the provided template instead
	// of the defaultTemplate constant.
	StackTemplateFilename string

	// The name of a previously created FlipTester
	// Cloudformation stack to resume using. Will
	// bypass any new stack creation and instead
	// simply provide the .Test() method against
	// the existing stack
	StackName string

	// A slice containing TestUrl structs
	// that the test will execute against. If
	// no test URLs are provided then a set of
	// defaults will be run.
	TestUrls []*TestUrl

	// Whether or not to retain the Cloudformation
	// stack after finishing the test. If the stack
	// is retained then the test can be run again
	// without having to wait for stack creation.
	RetainStack bool

	// The AWS session to use for this testing
	// process. If no session is provided then
	// one will be created using system defaults.
	Session *session.Session
}

// New returns an instance of FlipTester provided a prebuilt
// FlipTesterInput object. If not all parameters are defined then
// some default values will be chosen when possible. Will return a FlipTester
// from which the .Test() method can be called to create a
// Cloudformation stack, call the resulting lambda, and populate
// .TestResults with the results. The .TestEvent.TestUrls slice can
// be modified directly with custom tests before calling .Test() any
// errors will be returned.
func New(input *FlipTesterInput) (fliptester *FlipTester, err error) {
	if input.Session == nil {
		fliptester.sess = session.New()
	}
	ft := FlipTester{
		sess: input.Session,
	}
	if input.StackName == "" {
		// means we'll need a new stack
		if input.SubnetId == "" {
			err = errors.New("SubnetId is a required input field if StackName is not supplied")
			return fliptester, err
		}
		ft.subnetId = input.SubnetId
		if input.VpcId == "" {
			err = errors.New("VpcId is a required input field if StackName is not supplied")
			return fliptester, err
		}
		ft.vpcId = input.VpcId
		if input.StackPrefix == "" {
			ft.stackPrefix = DefaultStackPrefix
		} else {
			ft.stackPrefix = input.StackPrefix
		}
		ft.stackTemplateFilename = input.StackTemplateFilename
	} else {
		ft.log = append(ft.log, "using existing stack")
		ft.StackName = input.StackName
		ft.stackCreated = true
	}
	ft.RetainStack = input.RetainStack
	le := lambdaEvent{}
	ft.testEvent = &le
	if len(input.TestUrls) < 1 {
		// setup some defaults
		ft.testEvent.RequestType = "RunAll"
		ft.testEvent.TestUrls = append(ft.testEvent.TestUrls, &[]TestUrl{
			{
				Name: "gopkg.in",
				Url:  "https://gopkg.in",
			},
		}[0])
		ft.testEvent.TestUrls = append(ft.testEvent.TestUrls, &[]TestUrl{
			{
				Name: "google",
				Url:  "https://www.google.com",
			},
		}[0])
		ft.testEvent.TestUrls = append(ft.testEvent.TestUrls, &[]TestUrl{
			{
				Name: "time",
				Url:  "https://www.nist.gov",
			},
		}[0])
	} else {
		ft.testEvent.RequestType = "RunAll"
		ft.testEvent.TestUrls = input.TestUrls
	}
	return &ft, err
}

// FlipTester is object that is created and its methods are called
// in order to test internet in the VPC.
type FlipTester struct {
	subnetId              string
	vpcId                 string
	stackPrefix           string // e.g. "ISS-GR-egress-tester-"
	stackTemplateFilename string // e.g., "fliptest.yml"

	// Holds the list of URLs that will be passed to the
	// lambda when the .Test() method is called.
	TestUrls []*TestUrl

	// Stores results (if any) from tests after the
	// .Test() method has been called
	TestResults []*TestResult
	testEvent   *lambdaEvent

	// Indicates whether or not the tests passed. The pass
	// criteria is fixed based on whether the GET request
	// received a 200 response and it took less than 4 seconds
	Passed bool
	sess   *session.Session

	// Indicates whether or not the stack will be deleted after
	// the .Test() method is called.
	RetainStack  bool
	stackCreated bool

	// The stack name will be available here in case the tests need
	// to be resumed later.
	StackName    string
	functionName string
	log          []string
}

type lambdaEvent struct {
	RequestType string
	TestUrls    []*TestUrl
}

// TestResult holds results from the lambda execution.
type TestResult struct {
	Name         string
	ElapsedTimeS float64
	Message      string
	Success      bool
	Url          string
	ResponseCode int
}

// TestUrl holds a Name and Url. The Name is just
// an identifying label and a GET will be performed
// on the Url using the Python urllib library.
type TestUrl struct {
	Name string
	Url  string
}

func (ft *FlipTester) getTemplateBody() (body string, err error) {
	var bodyBytes []byte
	if ft.stackTemplateFilename == "" {
		return defaultTemplate, err
	}
	bodyBytes, err = ioutil.ReadFile(ft.stackTemplateFilename)
	return string(bodyBytes), err
}

func (ft *FlipTester) checkResults(results []*TestResult) (ok bool) {
	ok = true
	maxTime := 4.00000000
	for _, result := range results {
		if !result.Success || result.ElapsedTimeS > maxTime {
			msg := fmt.Sprintf("test failed or took too long: %s", result.Url)
			ft.log = append(ft.log, msg)
			ok = false
			return ok
		}
	}
	return ok
}

func (ft *FlipTester) callLamda() (err error) {
	// first make sure required info is retrieved from stack
	err = ft.getStackInfo()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(ft.testEvent)
	if err != nil {
		return err
	}
	inputInvoke := lambda.InvokeInput{
		FunctionName:   &ft.functionName,
		InvocationType: &[]string{"RequestResponse"}[0],
		Payload:        payload,
	}
	svcL := lambda.New(ft.sess)
	response, err := svcL.Invoke(&inputInvoke)
	if err != nil {
		return err
	}
	err = json.Unmarshal(response.Payload, &ft.TestResults)
	if err != nil {
		return err
	}
	ft.log = append(ft.log, "checking results for timing")
	if !ft.checkResults(ft.TestResults) {
		err = errors.New("tests failed or took too long")
	} else {
		ft.log = append(ft.log, "tests passed")
	}
	return err

}

// DeleteStack allows you to delete the Cloudformation
// stack manually.
func (ft *FlipTester) DeleteStack() (err error) {
	svc := cloudformation.New(ft.sess)

	input := &cloudformation.DeleteStackInput{
		StackName: &ft.StackName,
	}
	_, err = svc.DeleteStack(input)
	return err
}

func (ft *FlipTester) createStack() (err error) {
	svc := cloudformation.New(ft.sess)

	// try to read in the template file
	ft.log = append(ft.log, "loading template file")
	templateBody, err := ft.getTemplateBody()
	if err != nil {
		return err
	}
	// get random number to add into stack name
	rand.Seed(time.Now().UnixNano())
	rando := fmt.Sprintf("%08d", rand.Intn(10000000))
	input := &cloudformation.CreateStackInput{
		TimeoutInMinutes: &[]int64{3}[0],
		StackName:        &[]string{ft.stackPrefix + rando}[0],
		TemplateBody:     &templateBody,
		Capabilities: []*string{
			&[]string{"CAPABILITY_IAM"}[0],
			&[]string{"CAPABILITY_NAMED_IAM"}[0],
		},
		Parameters: []*cloudformation.Parameter{
			{
				ParameterKey:   &[]string{"SubnetId"}[0],
				ParameterValue: &[]string{ft.subnetId}[0],
			},
			{
				ParameterKey:   &[]string{"VpcId"}[0],
				ParameterValue: &[]string{ft.vpcId}[0],
			},
		},
	}
	response, err := svc.CreateStack(input)
	if err != nil {
		return err
	}
	ft.StackName = *response.StackId
	duration := time.Second * time.Duration(float64(5))
	time.Sleep(duration)
	stackID := response.StackId
	stack, err := ft.watchStack(stackID, 30)
	ft.StackName = *stack.StackName
	return err
}

func (ft *FlipTester) getStackInfo() (err error) {
	svc := cloudformation.New(ft.sess)
	input := cloudformation.DescribeStacksInput{
		StackName: &ft.StackName,
	}
	response, err := svc.DescribeStacks(&input)
	if err != nil {
		return err
	}
	if len(response.Stacks) > 0 {
		if len(response.Stacks[0].Outputs) > 0 {
			if *response.Stacks[0].Outputs[0].OutputKey == "FunctionName" {
				ft.functionName = *response.Stacks[0].Outputs[0].OutputValue
			} else {
				err = errors.New("error getting FunctionName output from existing stack")
				return err
			}
		} else {
			err = errors.New("no outputs detected on provided StackName")
			return err
		}
	} else {
		err = errors.New("could not find stack with provided StackName")
		return err
	}
	return err
}

// Test sets up the Cloudformation stack from template and then calls
// the created function and parses the results.
func (ft *FlipTester) Test() (err error) {
	ft.log = append(ft.log, "starting test")
	if !ft.stackCreated {
		ft.log = append(ft.log, "creating stack")
		err = ft.createStack()
		if err != nil {
			return err
		}
		// flag that the stack exists
		ft.stackCreated = true
	}
	if ft.stackCreated {
		ft.log = append(ft.log, "calling lambda")
		err = ft.callLamda()
		for i:= 0; i < 5; i++ {
			if err != nil {
				if strings.Contains(err.Error(), "Service") {
					// means we got that trash service exception
					// even though Cloudformation told us the lambda
					// was ready
					ft.log = append(ft.log, "service exception, sleeping and trying lambda again")
					duration := time.Second * time.Duration(float64(10))
					time.Sleep(duration)
					err = ft.callLamda()
				}
			}
		}
		if err != nil {
			return err
		}
		ft.Passed = true
	}
	if !ft.RetainStack {
		ft.log = append(ft.log, "deleting stack")
		err = ft.DeleteStack()
		if err == nil {
			ft.stackCreated = false
		}
	} else {
		ft.log = append(ft.log, "retaining stack")
	}
	if err != nil {
		ft.log = append(ft.log, "errors: "+err.Error())
		return err
	}
	ft.log = append(ft.log, "tests completed")
	return err
}

// GetLog returns a string representing the log messages
// from the life of the FlipTester object.
func (ft *FlipTester) GetLog() string {
	return strings.Join(ft.log, "\n")
}

func (ft *FlipTester) watchStack(
	stackID *string,
	maxtries int) (stack *cloudformation.Stack,
	err error) {
	svc := cloudformation.New(ft.sess)
	input := cloudformation.DescribeStacksInput{
		StackName: stackID,
	}
	count := 0
	for {
		count++
		if count > maxtries {
			break
		}
		ft.log = append(ft.log, "waiting on stack")

		result, err := svc.DescribeStacks(&input)
		if err != nil {
			return stack, err
		}
		if len(result.Stacks) > 0 {
			stack = result.Stacks[0]
			status := *stack.StackStatus
			switch status {
			case "CREATE_COMPLETE":
				return stack, err
			case "ROLLBACK_IN_PROGRESS":
				return stack, err
			case "ROLLBACK_COMPLETE":
				return stack, err
			case "DELETE_IN_PROGRESS":
				return stack, err
			case "DELETE_COMPLETE":
				return stack, err
			case "CREATE_FAILED":
				return stack, err
			case "DELETE_FAILED":
				return stack, err
			}
		} else {
			err = errors.New("not enough stacks in describe")
			return stack, err
		}
		duration := time.Second * time.Duration(float64(5))
		time.Sleep(duration)
	}
	return stack, err
}
