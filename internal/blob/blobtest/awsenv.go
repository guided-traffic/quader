package blobtest

import "os"

// EmptyAWSEnvironment empties what the AWS SDK's default chain reads of the machine: the region variables and the profile are
// unset, the shared config and credentials files point at nothing, and the instance metadata service is off. A package whose
// tests build a client of a connection with credentials_source default calls it from TestMain before m.Run, so that no test
// passes or fails by the region or the profile of the machine it runs on (a developer's ~/.aws/config names a region, a CI runner
// has none).
func EmptyAWSEnvironment() {
	for _, k := range []string{"AWS_REGION", "AWS_DEFAULT_REGION", "AWS_PROFILE", "AWS_DEFAULT_PROFILE"} {
		_ = os.Unsetenv(k)
	}
	for k, v := range map[string]string{
		"AWS_CONFIG_FILE":             os.DevNull,
		"AWS_SHARED_CREDENTIALS_FILE": os.DevNull,
		"AWS_EC2_METADATA_DISABLED":   "true",
	} {
		_ = os.Setenv(k, v)
	}
}
