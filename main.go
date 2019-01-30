package main

import (
	"log"
	"os"

	"github.com/almerlucke/go-elb-deploy/deploy"
)

func main() {
	curDir, err := os.Getwd()
	if err != nil {
		log.Fatalf("Deployment error: %v", err)
	}

	err = deploy.Deploy(curDir)
	if err != nil {
		log.Fatalf("Deployment error: %v", err)
	}

	log.Println("Deployed to AWS Elastic Beanstalk")
}
