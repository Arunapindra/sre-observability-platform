package test

import (
	"testing"

	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/stretchr/testify/assert"
)

func TestMonitoringModule(t *testing.T) {
	t.Parallel()

	terraformOptions := terraform.WithDefaultRetryableErrors(t, &terraform.Options{
		TerraformDir: "../",
		Vars: map[string]interface{}{
			"project_name":    "test-sre-platform",
			"environment":     "test",
			"eks_cluster_name": "test-cluster",
			"eks_oidc_issuer":  "https://oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE",
			"vpc_id":           "vpc-test123",
			"alert_email":      "test@example.com",
		},
	})

	defer terraform.Destroy(t, terraformOptions)
	terraform.InitAndPlan(t, terraformOptions)

	s3Bucket := terraform.Output(t, terraformOptions, "loki_storage_bucket")
	assert.Contains(t, s3Bucket, "test-sre-platform")

	snsTopicArn := terraform.Output(t, terraformOptions, "alert_sns_topic_arn")
	assert.Contains(t, snsTopicArn, "arn:aws:sns")
}
