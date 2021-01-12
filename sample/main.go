package main

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"

	"github.com/GESkunkworks/fliptest"
)

var (
	stackName = flag.String("stack-name", "",
		"stack name of previously created stack to resume usage. "+
			"Not setting this parameter will cause it to create a new stack.",
	)
	region = flag.String("region", "us-east-2",
		"Region to use for AWS API calls",
	)
	profile = flag.String("profile", "gr-partypod",
		"profile to use for AWS credentials",
	)
	vpcID = flag.String("vpc", "vpc-09228c82a4e14d978",
		"VPC ID of VPC to create lambda function in",
	)
	subnetID = flag.String("subnet", "subnet-0300fdf5b42dbf3ff",
		"Subnet ID of Subnet to create lambda function in",
	)
	createOnly = flag.Bool("create-only", false,
		"when this parameter is passed the stack will only be created "+
			"and no tests will run. Only works if no stack-name is passed.",
	)
)

func main() {
	flag.Parse()
	// setup session for flippage
	var sess *session.Session
	sess = session.Must(session.NewSessionWithOptions(session.Options{
		Config:  aws.Config{Region: aws.String(*region)},
		Profile: *profile,
	}))
	var err error
	var test *fliptest.FlipTester
	if *stackName != "" {
		fmt.Printf("resuming stack %s\n", *stackName)
		input := fliptest.FlipTesterInput{
			Session:                   sess,
			StackName:                 *stackName,
			InitialSleepTimeSeconds:   5,
			PostEventSleepTimeSeconds: 5,
			RetainStack:               true,
		}
		test, err = fliptest.New(&input)
		if err != nil {
			panic(err)
		}
		fmt.Println("Resuming stack....")
		err = test.Test()
		if err != nil {
			panic(err)
		}
	} else {
		fmt.Println("creating new stack")
		input := fliptest.FlipTesterInput{
			Session:                   sess,
			SubnetId:                  *subnetID,
			VpcId:                     *vpcID,
			InitialSleepTimeSeconds:   20,
			PostEventSleepTimeSeconds: 5,
			RetainStack:               true,
		}
		test, err = fliptest.New(&input)
		if err != nil {
			panic(err)
		}
		if *createOnly {
			fmt.Println("Launching stack only...")
			err = test.CreateStack()
		} else {
			fmt.Println("Launching stack and running tests....")
			err = test.Test()
		}
	}
	// if desired a simple activity log can be retrieved
	fmt.Println("Printing log:")
	fmt.Println(test.GetLog())
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
	}
}
