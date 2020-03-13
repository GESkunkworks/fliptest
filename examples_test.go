package fliptest_test

import (
	"encoding/json"
	"fmt"

	"github.com/GESkunkworks/fliptest"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
)

// resume-stack-with-custom-tests
//
// This example resumes an existing stack and changes
// the test URLs that it's calling on the lambda.
func ExampleNew_resumestackcustomtests() {
	var sess *session.Session
	sess = session.New()
	// if you retained the stack previously you can resume it
	// if you know the name
	var tests []*fliptest.TestUrl
	t1 := fliptest.TestUrl{
		Name: "cnn",
		Url:  "https://www.cnn.com/",
	}
	t2 := fliptest.TestUrl{
		Name: "mysite",
		Url:  "http://www.mysite.org/getdata/",
	}
	tests = append(tests, &t1)
	tests = append(tests, &t2)
	input := fliptest.FlipTesterInput{
		Session:     sess,
		StackName:   "ISS-GR-egress-tester-00714632",
		TestUrls:    tests,
		RetainStack: false,
	}
	test, err := fliptest.New(&input)
	if err != nil {
		panic(err)
	}
	err = test.Test()
	if err != nil {
		panic(err)
	}
	if body, err := json.MarshalIndent(test.TestResults, "", "    "); err == nil {
		fmt.Println(string(body))
	}
}

// new-stack
//
// This example sets up a new stack from scratch
// and retains it for future use.
func ExampleNew_newstack() {
	// setup session for flippage
	var sess *session.Session
	sess = session.Must(session.NewSessionWithOptions(session.Options{
		Config:  aws.Config{Region: aws.String("us-east-1")},
		Profile: "account1",
	}))

	input := fliptest.FlipTesterInput{
		Session:     sess,
		SubnetId:    "subnet-d3297188",
		VpcId:       "vpc-c8a6c3ae",
		RetainStack: true,
	}
	test, err := fliptest.New(&input)
	if err != nil {
		panic(err)
	}
	err = test.Test()
	if err != nil {
		fmt.Println(err)
		// if it was the tests failing that caused the error
		// we can see results from the test
		if body, err := json.MarshalIndent(test.TestResults, "", "    "); err == nil {
			fmt.Println(string(body))
		}
	} else {
		// see results of the tests that passed
		if body, err := json.MarshalIndent(test.TestResults, "", "    "); err == nil {
			fmt.Println(string(body))
		}
		// if desired a simple activity log can be retrieved
		fmt.Println(test.GetLog())
	}
}
