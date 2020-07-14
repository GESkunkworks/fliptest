package main

import (
    "fmt"
    "encoding/json"

    "github.com/aws/aws-sdk-go/aws"
    "github.com/aws/aws-sdk-go/aws/session"

    "github.com/GESkunkworks/fliptest"
)

func main() {
    // setup session for flippage
    var sess *session.Session
    sess = session.Must(session.NewSessionWithOptions(session.Options{
        Config:  aws.Config{Region: aws.String("us-east-1")},
        Profile: "my-account",
    }))

    input := fliptest.FlipTesterInput{
        Session:     sess,
        SubnetId:    "subnet-d4297188",
        VpcId:       "vpc-a8a6c3ad",
        RetainStack: true,
    }
    test, err := fliptest.New(&input)
    if err != nil {
        panic(err)
    }
	fmt.Println("Launching stack....")
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
    }
	// if desired a simple activity log can be retrieved
	fmt.Println("Printing log:")
	fmt.Println(test.GetLog())
}
