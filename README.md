## Automatic deployment of Go Docker files to Elastic Beanstalk

It is as simple as adding a deploy.json to your root project directory and run go-deploy inside the project directory.

The deploy file has the following structure.

    {
        "files": [
            "config",
            "controllers",
            "Dockerfile",
            "Dockerrun.aws.json",
            "errors",
            "models",
            "routes",
            "server.go",
            "services",
            "translations",
            "utils",
            "vendor"
        ],
        "aws": {
            "region": "eu-central-1",
            "s3": {
                "bucket": "my-build-bucket"
            },
            "credentials": {
                "accessKey": "MY-ACCESS-KEY",
                "secretAccessKey": "MY-SECRET-ACCESS-KEY"
            },
            "elb": {
                "applicationName": "MyApplicationName",
                "environmentName": "MyEnvironmentName"
            }
        },
        "branch": "master"
    }
    
In the files section you can specify which toplevel files/directories should be included in the deploy package. You must set an existing S3 bucket where the docker package will be uploaded so it can be deployed on ELB. The script will take the commit hash and branch to form the package name.

To work the project and deploy json MUST include a Dockerfile and Dockerrun.aws.json on root level. An example Dockerfile would be:

    # We want Golang 1.9 (so we can use the vendor references as created by godep)
    FROM golang:1.9

    # Copy the local package files to the container's workspace.
    ADD . /go/src/github.com/awesomecoder/goproject

    # Set the working directory of the container to our project
    # So relative paths can be found (like translations etc..)
    WORKDIR /go/src/github.com/awesomecoder/goproject

    # Run Go install for the Neurokeys project so the binary is created
    RUN go install github.com/awesomecoder/goproject

    # Run the Neurokeys command by default when the container starts.
    ENTRYPOINT /go/bin/goproject

    # Document that the service listens on port 3000.
    EXPOSE 3000

An example Dockerrun.aws.json would be:

    {
        "AWSEBDockerrunVersion": "1",
        "Ports": [
            {
                "ContainerPort": "3000"
            }
        ]
    }

