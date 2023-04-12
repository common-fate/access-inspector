# access-inspector

An entitlement analysis and remediation tool for Common Fate.

## Running

Requires Go 1.19+ and Python 3.9.

Install `cf`:

```bash
brew install common-fate/tap/cf
```

Create a folder for the analysis:

```bash
mkdir access-analysis
cd access-analysis
```

Clone and set up the AWS Access Provider:

```bash
git clone https://github.com/common-fate/cf-provider-aws.git
cd cf-provider-aws
python3 -m venv .venv
.venv/bin/pip install -r requirements.txt
cd ..
```

Clone this repo:

```bash
git clone https://github.com/common-fate/access-inspector.git
cd access-inspector
```

Log in to Common Fate:

```bash
cf login
```

Ensure that your terminal has active AWS credentials with access to AWS IAM Identity Center:

```bash
# for example:
export AWS_PROFILE=my-profile-with-access-to-aws-sso

# ensure that your AWS_REGION variable is set to the region that AWS IAM Identity Center is running in
export AWS_REGION=us-east-1 # for example - if IAM Identity Center runs in us-east-1
```

Configure the AWS provider:

```bash
export PROVIDER_CONFIG_SSO_IDENTITY_STORE_ID=$(aws sso-admin list-instances --query 'Instances[0].IdentityStoreId' --output text)
export PROVIDER_CONFIG_SSO_INSTANCE_ARN=$(aws sso-admin list-instances --query 'Instances[0].InstanceArn' --output text)
export PROVIDER_CONFIG_SSO_REGION=$AWS_REGION
export PROVIDER_CONFIG_SSO_ROLE_ARN=""
```

Query for AWS entitlements:

```bash
go run cmd/main.go scan --provider-local-path=../cf-provider-aws --output report.db
```

Query for active Access Requests within Common Fate:

```bash
go run cmd/main.go dump-requests --output requests.json
```

Analyze the permissions to plan persistent entitlements to remove (note - the below command doesn't actually remove anything, it's just a dry-run):

```bash
go run cmd/main.go analyze --report=report.db --requests=requests.json > cleanup.sh
```

Inspect the `cleanup.sh` script to verify the planned commands match your expectations:

```
cat cleanup.sh
```

The script should look something like this:

```bash
#!/bin/bash
SSO_INSTANCE_ARN=arn:aws:sso:::instance/ssoins-1234567890abcdef
SSO_REGION=ap-southeast-2

echo "(2/13) removing user chris@commonfate.io access to example-account (123456789012) with role AWSAdministratorAccess"
aws sso-admin delete-account-assignment --instance-arn $SSO_INSTANCE_ARN --region $SSO_REGION --target-type AWS_ACCOUNT --target-id 123456789012 --permission-set-arn arn:aws:sso:::permissionSet/ssoins-1234567890abcdef/ps-1234567890abcdef --principal-type USER --principal-id 1234512345-589828ee-abcde-abcd-abcd-1234512345
```

Run the script:

```bash
chmod +x cleanup.sh
./cleanup.sh
```
