# fliptest

Package fliptest provides a mechanism for testing internet egress in an AWS VPC by creating a VPC Lambda via Cloudformation stack to which custom test URLs can be passed.

Sample usage:

```go
package main

import (
    "fmt"
    "encoding/json"

    "github.com/GESkunkworks/fliptest"
)

func main() {
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
```