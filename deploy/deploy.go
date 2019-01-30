package deploy

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/elasticbeanstalk"
	"github.com/aws/aws-sdk-go/service/s3"
)

/*
 *
 *	_progressFunc is the type of the function called for each archive file.
 *
 */

type _progressFunc func(archivePath string)

/*
 *
 *	Internal deployment descriptor struct
 *
 */

type _deploymentDescriptor struct {
	Files []string `json:"files"`
	AWS   struct {
		Region      string `json:"region"`
		Credentials struct {
			AccessKey       string `json:"accessKey"`
			SecretAccessKey string `json:"secretAccessKey"`
		} `json:"credentials"`
		S3 struct {
			Bucket string `json:"bucket"`
		} `json:"s3"`
		ELB struct {
			ApplicationName string `json:"applicationName"`
			EnvironmentName string `json:"environmentName"`
		} `json:"elb"`
	} `json:"aws"`
	Branch       string           `json:"branch"`
	BuildVersion string           `json:"-"`
	BuildKey     string           `json:"-"`
	Directory    string           `json:"-"`
	CommitHash   string           `json:"-"`
	Session      *session.Session `json:"-"`
}

/*
 *
 *	Create deployment descriptor
 *
 */

func newDeploymentDescriptor(deploymentDirectory string) (*_deploymentDescriptor, error) {
	file, err := os.Open(filepath.Join(deploymentDirectory, "deploy.json"))
	if err != nil {
		return nil, err
	}

	defer file.Close()

	descriptor := &_deploymentDescriptor{}

	err = json.NewDecoder(file).Decode(&descriptor)
	if err != nil {
		return nil, err
	}

	descriptor.Directory = deploymentDirectory

	commitHash, err := descriptor.getCommitHash()
	if err != nil {
		return nil, err
	}

	descriptor.CommitHash = commitHash
	descriptor.BuildVersion = fmt.Sprintf("%v-%v", descriptor.Branch, commitHash)
	descriptor.BuildKey = descriptor.BuildVersion + ".zip"
	descriptor.Session = session.New(&aws.Config{
		Credentials: credentials.NewStaticCredentials(
			descriptor.AWS.Credentials.AccessKey,
			descriptor.AWS.Credentials.SecretAccessKey,
			""),
		Region:           aws.String(descriptor.AWS.Region),
		S3ForcePathStyle: aws.Bool(true),
	})

	return descriptor, nil
}

/*
 *
 *	Get commit hash from HEAD of branch in deploy.json
 *
 */

func (descriptor *_deploymentDescriptor) getCommitHash() (string, error) {
	headPath := filepath.Join(descriptor.Directory, ".git", "refs", "heads", descriptor.Branch)

	content, err := ioutil.ReadFile(headPath)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(content)), nil
}

/*
 *
 *	Create zip
 *
 */

func writeDirToZip(wr *zip.Writer, dirPath string, progress _progressFunc) error {
	basePath := filepath.Base(dirPath)

	err := filepath.Walk(dirPath, func(filePath string, fileInfo os.FileInfo, err error) error {
		if err != nil || fileInfo.IsDir() {
			return err
		}

		relativeFilePath, err := filepath.Rel(dirPath, filePath)
		if err != nil {
			return err
		}

		archivePath := path.Join(basePath, path.Join(filepath.SplitList(relativeFilePath)...))

		if progress != nil {
			progress(archivePath)
		}

		file, err := os.Open(filePath)
		if err != nil {
			return err
		}
		defer func() {
			_ = file.Close()
		}()

		zipFileWriter, err := wr.Create(archivePath)
		if err != nil {
			return err
		}

		_, err = io.Copy(zipFileWriter, file)
		return err
	})

	return err
}

func writeFileToZip(wr *zip.Writer, filePath string, progress _progressFunc) error {
	basePath := filepath.Base(filePath)
	if progress != nil {
		progress(basePath)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()

	zipFileWriter, err := wr.Create(basePath)
	if err != nil {
		return err
	}

	_, err = io.Copy(zipFileWriter, file)
	return err
}

func (descriptor *_deploymentDescriptor) zipFiles(progress _progressFunc) (*bytes.Buffer, error) {
	// Create zip buffer
	zipBuffer := new(bytes.Buffer)
	zipWriter := zip.NewWriter(zipBuffer)
	deployDir := descriptor.Directory

	for _, deployFile := range descriptor.Files {
		filePath := filepath.Join(deployDir, deployFile)
		info, err := os.Lstat(filePath)
		if err != nil {
			return nil, err
		}

		if info.IsDir() {
			err = writeDirToZip(zipWriter, filePath, progress)
		} else {
			err = writeFileToZip(zipWriter, filePath, progress)
		}

		if err != nil {
			return nil, err
		}
	}

	err := zipWriter.Close()
	if err != nil {
		return nil, err
	}

	return zipBuffer, nil
}

/*
 *
 *	Create zip and upload to S3
 *
 */
func (descriptor *_deploymentDescriptor) uploadToS3() error {
	zipBuffer, err := descriptor.zipFiles(nil)
	if err != nil {
		return err
	}

	s3client := s3.New(descriptor.Session)

	params := &s3.PutObjectInput{
		Bucket:        aws.String(descriptor.AWS.S3.Bucket),
		Key:           aws.String(descriptor.BuildKey),
		ACL:           aws.String("private"),
		Body:          bytes.NewReader(zipBuffer.Bytes()),
		ContentLength: aws.Int64(int64(zipBuffer.Len())),
		ContentType:   aws.String("application/zip"),
		Metadata: map[string]*string{
			"Key": aws.String("MetadataValue"),
		},
	}

	_, err = s3client.PutObject(params)
	if err != nil {
		return err
	}

	return nil
}

/*
 *
 *	Deploy to beanstalk, first create application version, then update
 *	environment
 *
 */
func (descriptor *_deploymentDescriptor) elasticBeanstalkDeploy() error {
	// Create elastic beanstalk client
	elbclient := elasticbeanstalk.New(descriptor.Session)

	// Create application version input
	versionInput := &elasticbeanstalk.CreateApplicationVersionInput{
		ApplicationName: aws.String(descriptor.AWS.ELB.ApplicationName),
		VersionLabel:    aws.String(descriptor.BuildVersion),
		SourceBundle: &elasticbeanstalk.S3Location{
			S3Bucket: aws.String(descriptor.AWS.S3.Bucket),
			S3Key:    aws.String(descriptor.BuildKey),
		},
	}

	// Create application version
	_, err := elbclient.CreateApplicationVersion(versionInput)
	if err != nil {
		return err
	}

	// Create update environment input
	environmentInput := &elasticbeanstalk.UpdateEnvironmentInput{
		VersionLabel:    aws.String(descriptor.BuildVersion),
		EnvironmentName: aws.String(descriptor.AWS.ELB.EnvironmentName),
	}

	// Update environment
	_, err = elbclient.UpdateEnvironment(environmentInput)
	if err != nil {
		return err
	}

	return nil
}

// Deploy Docker zip automatically to Elastic Beanstalk with input from deploy.json:
// - zip files
// - upload zip to S3 bucket
// - create ELB application version
// - update ELB environment with application version
func Deploy(deploymentDirectory string) error {
	desc, err := newDeploymentDescriptor(deploymentDirectory)
	if err != nil {
		return err
	}

	err = desc.uploadToS3()
	if err != nil {
		return err
	}

	err = desc.elasticBeanstalkDeploy()
	if err != nil {
		return err
	}

	return nil
}
