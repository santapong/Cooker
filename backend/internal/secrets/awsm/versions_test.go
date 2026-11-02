package awsm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/santapong/cooker/internal/secrets"
)

const versionOne = "11111111111111111111111111111111"
const versionTwo = "22222222222222222222222222222222"
const arnOne = "arn:aws:secretsmanager:us-east-1:123456789012:secret:cooker/env/PASSWORD-AbC123"
const awsCanary = "SECRET_CANARY_AWS_VALUE"

type fakeVersions struct {
	describe *secretsmanager.DescribeSecretOutput
	value    *secretsmanager.GetSecretValueOutput
	err      error
	calls    int
	request  *secretsmanager.GetSecretValueInput
}

func (f *fakeVersions) DescribeSecret(context.Context, *secretsmanager.DescribeSecretInput, ...func(*secretsmanager.Options)) (*secretsmanager.DescribeSecretOutput, error) {
	f.calls++
	return f.describe, f.err
}
func (f *fakeVersions) GetSecretValue(_ context.Context, in *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	f.calls++
	f.request = in
	return f.value, f.err
}
func fixture() (*Manager, *fakeVersions) {
	f := &fakeVersions{describe: &secretsmanager.DescribeSecretOutput{ARN: aws.String(arnOne), Name: aws.String("cooker/env/PASSWORD"), VersionIdsToStages: map[string][]string{versionOne: {"AWSCURRENT"}}}, value: &secretsmanager.GetSecretValueOutput{ARN: aws.String(arnOne), Name: aws.String("cooker/env/PASSWORD"), VersionId: aws.String(versionOne), SecretString: aws.String(awsCanary)}}
	return &Manager{prefix: "cooker", region: "us-east-1", versions: f}, f
}
func TestAWSPinsExactVersionAcrossRotation(t *testing.T) {
	m, f := fixture()
	ctx := context.Background()
	r, err := m.Pin(ctx, "env", "PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	f.describe.VersionIdsToStages = map[string][]string{versionTwo: {"AWSCURRENT"}, versionOne: {"AWSPREVIOUS"}}
	b, err := m.GetPinned(ctx, "env", "PASSWORD", r)
	if err != nil || string(b) != awsCanary {
		t.Fatal("pinned read failed", err)
	}
	if aws.ToString(f.request.SecretId) != arnOne || aws.ToString(f.request.VersionId) != versionOne || f.request.VersionStage != nil {
		t.Fatal("unpinned request")
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), awsCanary) {
		t.Fatal("value in reference")
	}
}
func TestAWSRejectsScopeBeforeRead(t *testing.T) {
	for _, field := range []string{"authority", "region", "prefix", "provider", "env", "key"} {
		t.Run(field, func(t *testing.T) {
			m, f := fixture()
			r, err := m.Pin(context.Background(), "env", "PASSWORD")
			if err != nil {
				t.Fatal(err)
			}
			f.calls = 0
			switch field {
			case "authority":
				m.authority = "https://other.test"
			case "region":
				m.region = "us-west-2"
			case "prefix":
				m.prefix = "other"
			case "provider":
				r.Provider = "gcpsm"
			case "env":
				r.Scope.EnvironmentID = "other"
			case "key":
				r.Scope.Key = "other"
			}
			if _, err := m.GetPinned(context.Background(), "env", "PASSWORD", r); !errors.Is(err, secrets.ErrVersionMismatch) {
				t.Fatal(err)
			}
			if f.calls != 0 {
				t.Fatal("read before scope fence")
			}
		})
	}
}
func TestAWSDeletedRecreatedAndMalformedFailClosed(t *testing.T) {
	for _, which := range []string{"deleted", "recreated", "version", "nil", "missing-value", "both-values", "provider-error"} {
		t.Run(which, func(t *testing.T) {
			m, f := fixture()
			r, err := m.Pin(context.Background(), "env", "PASSWORD")
			if err != nil {
				t.Fatal(err)
			}
			switch which {
			case "deleted", "provider-error":
				f.err = errors.New(awsCanary)
			case "recreated":
				f.value.ARN = aws.String(strings.Replace(arnOne, "AbC123", "NEW123", 1))
			case "version":
				f.value.VersionId = aws.String(versionTwo)
			case "nil":
				f.value = nil
			case "missing-value":
				f.value.SecretString = nil
			case "both-values":
				f.value.SecretBinary = []byte(awsCanary)
			}
			b, err := m.GetPinned(context.Background(), "env", "PASSWORD", r)
			if err == nil || b != nil || strings.Contains(err.Error(), awsCanary) {
				t.Fatal("unsafe failure")
			}
		})
	}
}
func TestAWSPinRejectsAmbiguousAndMalformedMetadata(t *testing.T) {
	for _, which := range []string{"duplicate-current", "bad-version", "partial-arn", "wrong-region", "wrong-name", "nil", "raw-error"} {
		t.Run(which, func(t *testing.T) {
			m, f := fixture()
			switch which {
			case "duplicate-current":
				f.describe.VersionIdsToStages[versionTwo] = []string{"AWSCURRENT"}
			case "bad-version":
				f.describe.VersionIdsToStages = map[string][]string{awsCanary: {"AWSCURRENT"}}
			case "partial-arn":
				f.describe.ARN = aws.String(strings.TrimSuffix(arnOne, "-AbC123"))
			case "wrong-region":
				f.describe.ARN = aws.String(strings.Replace(arnOne, "us-east-1", "us-west-2", 1))
			case "wrong-name":
				f.describe.Name = aws.String(awsCanary)
			case "nil":
				f.describe = nil
			case "raw-error":
				f.err = errors.New(awsCanary)
			}
			r, err := m.Pin(context.Background(), "env", "PASSWORD")
			if err == nil || r.Resource != "" || strings.Contains(err.Error(), awsCanary) {
				t.Fatal("unsafe metadata")
			}
		})
	}
}
func TestAWSBinaryAndEmptyValues(t *testing.T) {
	for _, binary := range []bool{false, true} {
		m, f := fixture()
		r, _ := m.Pin(context.Background(), "env", "PASSWORD")
		if binary {
			f.value.SecretString = nil
			f.value.SecretBinary = []byte{}
		} else {
			f.value.SecretString = aws.String("")
		}
		b, err := m.GetPinned(context.Background(), "env", "PASSWORD", r)
		if err != nil || b == nil || len(b) != 0 {
			t.Fatal("empty value rejected", err)
		}
	}
}

func TestAWSEffectiveEndpointBinding(t *testing.T) {
	if versionAuthority(nil) != "aws-secrets-manager" {
		t.Fatal("default authority")
	}
	if versionAuthority(aws.String("https://EXAMPLE.test/")) != "https://example.test" {
		t.Fatal("custom endpoint")
	}
	m, f := fixture()
	m.authority = versionAuthority(aws.String("https://user:private@example.test"))
	if _, err := m.Pin(context.Background(), "env", "PASSWORD"); !errors.Is(err, secrets.ErrInvalidVersionReference) || f.calls != 0 {
		t.Fatal("invalid endpoint read")
	}
}
