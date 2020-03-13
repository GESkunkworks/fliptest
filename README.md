# fliptest

Package fliptest provides a mechanism for testing internet egress in an AWS VPC by creating a VPC Lambda via Cloudformation stack to which custom test URLs can be passed.

Read the docs: [https://godoc.org/github.com/GESkunkworks/fliptest](https://godoc.org/github.com/GESkunkworks/fliptest)

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

sample output:

```
$ go run main.go
[
    {
        "Name": "gopkg.in",
        "ElapsedTimeS": 0.49933934211730957,
        "Message": "got response code from URL",
        "Success": true,
        "Url": "https://gopkg.in",
        "ResponseCode": 200
    },
    {
        "Name": "google",
        "ElapsedTimeS": 0.690319299697876,
        "Message": "got response code from URL",
        "Success": true,
        "Url": "https://www.google.com",
        "ResponseCode": 200
    },
    {
        "Name": "time",
        "ElapsedTimeS": 0.9288179874420166,
        "Message": "got response code from URL",
        "Success": true,
        "Url": "https://www.nist.gov",
        "ResponseCode": 200
    }
]
starting test
creating stack
loading template file
waiting on stack
waiting on stack
waiting on stack
waiting on stack
waiting on stack
waiting on stack
waiting on stack
calling lambda
tests passed
retaining stack
tests completed
```
