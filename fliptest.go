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

	// The context that will be added to all log
	// messages. Generally the account name
	// or something similar
	Context string

	// How long the tester should sleep (in seconds)
	// before attempting to call the lambda after
	// it detects the stack is created. In some
	// regions this can take up to 40 seconds.
	// Default: 40 Seconds
	InitialSleepTimeSeconds int

	// How long after creating the test event
	// to sleep (in seconds). Sometimes these
	// VPC lambdas need a little extra time.
	// Default: 20 Seconds
	PostEventSleepTimeSeconds int
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
	if input.Context == "" {
		input.Context = "Default"
	}
	ft.context = input.Context
	if input.InitialSleepTimeSeconds == 0 {
		input.InitialSleepTimeSeconds = 40
	}
	ft.initialSleepTimeSeconds = input.InitialSleepTimeSeconds
	if input.PostEventSleepTimeSeconds == 0 {
		input.PostEventSleepTimeSeconds = 20
	}
	ft.postEventSleepTimeSeconds = input.PostEventSleepTimeSeconds
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
		msg := "using existing stack"
		ft.logMessage(msg)
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
	StackName                 string
	functionName              string
	log                       []string
	context                   string // identifier used in logging e.g. account name
	initialSleepTimeSeconds   int    // how long after stack is "ready" to sleep
	postEventSleepTimeSeconds int    // how long after test event creation to sleep
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

func (ft *FlipTester) logMessage(msg string) {
	t := time.Now()
	tString := t.Format(time.RFC3339)
	rMsg := fmt.Sprintf("%s: Context: '%s', StackName: '%s', Message: '%s'",
		tString, ft.context, ft.StackName, msg,
	)
	ft.log = append(ft.log, rMsg)
}

func (ft *FlipTester) getTemplateBody() (body string, err error) {
	var bodyBytes []byte
	if ft.stackTemplateFilename == "" {
		return defaultTemplate, err
	}
	bodyBytes, err = ioutil.ReadFile(ft.stackTemplateFilename)
	return string(bodyBytes), err
}

func (ft *FlipTester) checkResults(results []*TestResult) (ok bool, err error) {
	ok = true
	maxTime := 6.00000000
	for _, result := range results {
		if !result.Success {
			msg := fmt.Sprintf("test failed: %s", result.Url)
			ft.logMessage(msg)
			err = errors.New(msg)
			ok = false
			return ok, err
		} else if result.ElapsedTimeS > maxTime {
			msg := fmt.Sprintf("test took too long: %s", result.Url)
			ft.logMessage(msg)
			err = errors.New(msg)
			ok = false
			return ok, err
		}
	}
	return ok, err
}

func (ft *FlipTester) callLamda() (err error) {
	msg := "inside callLambda"
	ft.logMessage(msg)
	// first make sure required info is retrieved from stack
	err = ft.getStackInfo()
	if err != nil {
		return err
	}
	msg = "preparing test event"
	ft.logMessage(msg)
	payload, err := json.Marshal(ft.testEvent)
	if err != nil {
		return err
	}
	inputInvoke := lambda.InvokeInput{
		FunctionName:   &ft.functionName,
		InvocationType: &[]string{"RequestResponse"}[0],
		Payload:        payload,
	}

	msg = fmt.Sprintf("sleeping %ds before invoking lambda", ft.postEventSleepTimeSeconds)
	ft.logMessage(msg)
	duration := time.Second * time.Duration(float64(ft.postEventSleepTimeSeconds))
	time.Sleep(duration)
	msg = "invoking lambda"
	ft.logMessage(msg)
	svcL := lambda.New(ft.sess)
	response, err := svcL.Invoke(&inputInvoke)
	if err != nil {
		return err
	}
	err = json.Unmarshal(response.Payload, &ft.TestResults)
	if err != nil {
		return err
	}
	msg = "checking results for timing"
	ft.logMessage(msg)
	_, err = ft.checkResults(ft.TestResults)
	if err != nil {
		err = errors.New("tests failed or took too long")
	}
	msg = "tests passed"
	ft.logMessage(msg)
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

// CreateStack takes the current fliptest session information and
// creates the test stack in the desired VPC/Subnet. It blocks
// until the stack is fully created and ready and returns any errors.
func (ft *FlipTester) CreateStack() (err error) {
	svc := cloudformation.New(ft.sess)

	// try to read in the template file
	msg := "loading template file"
	ft.logMessage(msg)
	templateBody, err := ft.getTemplateBody()
	if err != nil {
		return err
	}
	// get random number to add into stack name
	rand.Seed(time.Now().UnixNano())
	rando := fmt.Sprintf("%08d", rand.Intn(10000000))
	stackName := ft.stackPrefix + rando
	var tInt int64
	tInt = 15
	input := &cloudformation.CreateStackInput{
		TimeoutInMinutes: &tInt,
		StackName:        &stackName,
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
	msg = fmt.Sprintf("creating stack with name '%s'", stackName)
	ft.logMessage(msg)
	response, err := svc.CreateStack(input)
	if err != nil {
		return err
	}
	ft.StackName = *response.StackId
	duration := time.Second * time.Duration(float64(5))
	time.Sleep(duration)
	stackID := response.StackId
	stack, err := ft.watchStack(stackID, 90)
	if err != nil {
		return err
	}
	ft.StackName = *stack.StackName
	ft.stackCreated = true
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
	msg := "starting test"
	ft.logMessage(msg)
	if !ft.stackCreated {
		msg = "stack doesn't exist yet, creating stack"
		ft.logMessage(msg)
		err = ft.CreateStack()
		if err != nil {
			return err
		}
	}
	if ft.stackCreated {
		duration := time.Second * time.Duration(float64(ft.initialSleepTimeSeconds))
		msg = fmt.Sprintf("sleeping %d seconds before calling lambda", ft.initialSleepTimeSeconds)
		ft.logMessage(msg)
		time.Sleep(duration)
		msg = "calling lambda"
		ft.logMessage(msg)
		err = ft.callLamda()
		msg = "called lambda, processing errors"
		ft.logMessage(msg)
		for i := 0; i < 5; i++ {
			if err != nil {
				if strings.Contains(err.Error(), "Service") {
					// means we got that trash service exception
					// even though Cloudformation told us the lambda
					// was ready
					msg = "service exception, sleeping and trying lambda again"
					ft.logMessage(msg)
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
		msg = "deleting stack"
		ft.logMessage(msg)
		err = ft.DeleteStack()
		if err == nil {
			ft.stackCreated = false
		}
	} else {
		msg = "retaining stack"
		ft.logMessage(msg)
	}
	if err != nil {
		msg := fmt.Sprintf("errors: %s", err.Error())
		ft.logMessage(msg)
		return err
	}
	msg = "tests complete"
	ft.logMessage(msg)
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
		msg := "waiting on stack"
		ft.logMessage(msg)

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
		duration := time.Second * time.Duration(float64(10))
		time.Sleep(duration)
	}
	return stack, err
}
