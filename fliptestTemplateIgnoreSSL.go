package fliptest

const ignoreSSLTemplate string = `
---
AWSTemplateFormatVersion: '2010-09-09'
Description: 'Stack to launch a vpc lambda to test Internet in a VPC'
Parameters:
  SubnetId: 
    Description: The subnet id to deploy the lambda into
    Type: String
  VpcId: 
    Description: The vpc to deploy the lambda into
    Type: String

Resources:
  TestInternetFunction:
    Type: AWS::Lambda::Function
    Properties:
      Code:
        ZipFile: |
          import json
          import time
          import urllib
          import ssl

          class UrlTimer:
              def __init__(self,name,url):
                  self.name = name
                  self.starttime = time.time()
                  self.elapsed = ""
                  self.message = ""
                  self.success = False
                  self.url = url
                  self.response_code = 0
                  self.dict = {}
              def exec(self):
                  try:
                      ctx = ssl.create_default_context()
                      ctx.check_hostname = False
                      ctx.verify_mode = ssl.CERT_NONE
                      response = urllib.request.urlopen(self.url, context=ctx, timeout=4)
                      self.response_code = response.getcode()
                      self.success = True
                      self.message = "got response code from URL"
                  except Exception as e:
                      self.message = "problem getting URL: " + str(e)
                  return self.report()
              def dictify(self):
                  self.dict = {
                      "Name": self.name,
                      "ElapsedTimeS": self.elapsed,
                      "Message": self.message,
                      "Success": self.success,
                      "Url": self.url,
                      "ResponseCode": self.response_code,
                  }
              def report(self):
                  self.elapsed = time.time() - self.starttime
                  self.dictify()
                  return json.dumps(self.dict)
          def handler(event, context):
                  tests = []
                  total_time = float(0)
                  response = []
                  if event.get("TestUrls") is not None:
                          # means user passed custom tests
                          for test in event["TestUrls"]:
                                  tests.append(UrlTimer(
                                    test.get("Name"),
                                    test.get("Url"),
                                    )
                                  )

                  else:
                          # run some default tests
                          tests.append(UrlTimer("gopkg","http://gopkg.in"))
                          tests.append(UrlTimer("google","http://www.google.com"))
                  if event["RequestType"] in ["RunAll"]:
                      for test in tests:
                          print(test.exec())
                          total_time += test.elapsed
                          response.append(test.dict)
                  return(response)

      Handler: "index.handler"
      Role:
        Fn::GetAtt:
        - LambdaExecutionRole
        - Arn
      Runtime: python3.9
      Timeout: '30'
      VpcConfig:
        SecurityGroupIds:
          - Ref: SecurityGroup
        SubnetIds:
          - Ref: SubnetId
            
  LambdaExecutionRole:
    Type: AWS::IAM::Role
    Properties:
      ManagedPolicyArns:
      - "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
      AssumeRolePolicyDocument:
        Version: '2012-10-17'
        Statement:
        - Effect: Allow
          Principal:
            Service:
            - lambda.amazonaws.com
          Action:
          - sts:AssumeRole
      Path: "/cs/"

  SecurityGroup:
    Type: AWS::EC2::SecurityGroup
    Properties:
      GroupDescription: for nat relaunch test internet lambda function 
      VpcId: 
        Ref: VpcId 

Outputs:
  FunctionName:
    Description: The name of the lambda function that was created
    Value: !Ref TestInternetFunction
...
`
