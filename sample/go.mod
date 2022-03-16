module fliptest-example

go 1.17

replace github.com/GESkunkworks/fliptest v1.0.15 => ../

require (
	github.com/GESkunkworks/fliptest v1.0.15
	github.com/aws/aws-sdk-go v1.43.3
)

require github.com/jmespath/go-jmespath v0.4.0 // indirect
