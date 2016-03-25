package aws

import (
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/hashicorp/terraform/helper/schema"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/elasticbeanstalk"
)

func resourceAwsElasticBeanstalkApplicationVersion() *schema.Resource {
	return &schema.Resource{
		Create: resourceAwsElasticBeanstalkApplicationVersionCreate,
		Read:   resourceAwsElasticBeanstalkApplicationVersionRead,
		Update: resourceAwsElasticBeanstalkApplicationVersionUpdate,
		Delete: resourceAwsElasticBeanstalkApplicationVersionDelete,

		Schema: map[string]*schema.Schema{
			"application": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"description": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"bucket": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"key": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"retention_number": &schema.Schema{
				Type:     schema.TypeInt,
				Optional: true,
			},
			"retention_period": &schema.Schema{
				Type:     schema.TypeInt,
				Optional: true,
			},
		},
	}
}

func resourceAwsElasticBeanstalkApplicationVersionCreate(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).elasticbeanstalkconn

	application := d.Get("application").(string)
	description := d.Get("description").(string)
	bucket := d.Get("bucket").(string)
	key := d.Get("key").(string)
	name := d.Get("name").(string)

	s3Location := elasticbeanstalk.S3Location{
		S3Bucket: aws.String(bucket),
		S3Key:    aws.String(key),
	}

	createOpts := elasticbeanstalk.CreateApplicationVersionInput{
		ApplicationName: aws.String(application),
		Description:     aws.String(description),
		SourceBundle:    &s3Location,
		VersionLabel:    aws.String(name),
	}

	log.Printf("[DEBUG] Elastic Beanstalk Application Version create opts: %s", createOpts)
	_, err := conn.CreateApplicationVersion(&createOpts)
	if err != nil {
		return err
	}

	d.SetId(name)
	log.Printf("[INFO] Elastic Beanstalk Application Version Label: %s", name)

	return resourceAwsElasticBeanstalkApplicationVersionRead(d, meta)
}

func resourceAwsElasticBeanstalkApplicationVersionRead(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).elasticbeanstalkconn

	resp, err := conn.DescribeApplicationVersions(&elasticbeanstalk.DescribeApplicationVersionsInput{
		VersionLabels: []*string{aws.String(d.Id())},
	})

	if err != nil {
		return err
	}

	if len(resp.ApplicationVersions) == 0 {
		log.Printf("[DEBUG] Elastic Beanstalk application version read: application version not found")

		d.SetId("")

		return nil
	} else if len(resp.ApplicationVersions) != 1 {
		return fmt.Errorf("Error reading application version properties: found %d application versions, expected 1", len(resp.ApplicationVersions))
	}

	if err := d.Set("description", resp.ApplicationVersions[0].Description); err != nil {
		return err
	}

	return nil
}

func resourceAwsElasticBeanstalkApplicationVersionUpdate(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).elasticbeanstalkconn

	if d.HasChange("description") {
		if err := resourceAwsElasticBeanstalkApplicationVersionDescriptionUpdate(conn, d); err != nil {
			return err
		}
	}

	return resourceAwsElasticBeanstalkApplicationVersionRead(d, meta)

}

func resourceAwsElasticBeanstalkApplicationVersionDescriptionUpdate(conn *elasticbeanstalk.ElasticBeanstalk, d *schema.ResourceData) error {
	application := d.Get("application").(string)
	description := d.Get("description").(string)
	name := d.Get("name").(string)

	log.Printf("[DEBUG] Elastic Beanstalk application version: %s, update description: %s", name, description)

	_, err := conn.UpdateApplicationVersion(&elasticbeanstalk.UpdateApplicationVersionInput{
		ApplicationName: aws.String(application),
		Description:     aws.String(description),
		VersionLabel:    aws.String(name),
	})

	return err
}

func resourceAwsElasticBeanstalkApplicationVersionDelete(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).elasticbeanstalkconn

	application := d.Get("application").(string)
	name := d.Id()
	retentionNumber := d.Get("retention_number").(int)
	retentionPeriod := d.Get("retention_period").(int)

	if retentionNumber == 0 {
		log.Printf("[DEBUG] retentionNumber and retentionPeriod not set. Deleteting %s", name)
		if err := deleteApplicationVersion(conn, application, name); err != nil {
			return err
		}
	} else {
		log.Printf("[DEBUG] retentionNumber: %d retentionPeriod: %d", retentionNumber, retentionPeriod)
		versions, err := conn.DescribeApplicationVersions(&elasticbeanstalk.DescribeApplicationVersionsInput{
			ApplicationName: aws.String(application),
		})

		if err != nil {
			return err
		}

		for _, v := range applicationVersions(versions.ApplicationVersions, retentionNumber, retentionPeriod) {
			if err = deleteApplicationVersion(conn, application, *v); err != nil {
				return err
			}
		}
	}
	d.SetId("")
	return nil
}

func deleteApplicationVersion(conn *elasticbeanstalk.ElasticBeanstalk, application string, v string) error {
	log.Printf("[DEBUG] Deleting Application Version: %s", v)
	_, err := conn.DeleteApplicationVersion(&elasticbeanstalk.DeleteApplicationVersionInput{
		ApplicationName: aws.String(application),
		VersionLabel:    aws.String(v),
	})

	if err != nil {
		if awserr, ok := err.(awserr.Error); ok {
			// application version is pending delete, or no longer exists.
			if awserr.Code() == "InvalidParameterValue" {
				return nil
			}
		}
		return err
	}
	return nil
}

func applicationVersions(versions []*elasticbeanstalk.ApplicationVersionDescription, retentionNumber int, retentionPeriod int) []*string {
	var versionsToDelete []*string
	retentionPeriodHours := time.Duration(retentionPeriod) * time.Hour

	versionSlice := applicationVersionDescriptionSlice(versions)
	log.Printf("[DEBUG] Pre-Sorted Elastic Beanstalk Application Versions %v", &versionSlice)
	sort.Sort(versionSlice)
	log.Printf("[DEBUG] Sorted Elastic Beanstalk Application Versions %v", &versionSlice)

	if retentionNumber != 0 {
		// When the number of application versions is less than retention number, don't delete anything.
		if len(versionSlice) <= retentionNumber {
			return nil
		}
		versionSlice = versionSlice[retentionNumber:]
	}

	for _, v := range versionSlice {
		if retentionPeriod != 0 {
			if time.Since(*v.DateCreated) > retentionPeriodHours {
				versionsToDelete = append(versionsToDelete, v.VersionLabel)
			}
		} else {
			versionsToDelete = append(versionsToDelete, v.VersionLabel)
		}
	}

	log.Printf("[DEBUG] Elastic Beanstalk Application Versions to delete %v", &versionsToDelete)
	return versionsToDelete
}

// To make sure the application versions are always sorted we implement the sort interface
// for our local ApplicationVersionDescription slice type. Sort order is most recent to oldest.
type applicationVersionDescriptionSlice []*elasticbeanstalk.ApplicationVersionDescription

func (slice applicationVersionDescriptionSlice) Len() int {
	return len(slice)
}

func (slice applicationVersionDescriptionSlice) Less(i, j int) bool {
	return slice[i].DateCreated.After(*slice[j].DateCreated)
}

func (slice applicationVersionDescriptionSlice) Swap(i, j int) {
	slice[i], slice[j] = slice[j], slice[i]
}
