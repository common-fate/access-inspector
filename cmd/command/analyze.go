package command

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/common-fate/clio"
	"github.com/common-fate/provider-registry-sdk-go/pkg/providerregistrysdk"
	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

type groupAssignment struct {
	AccountAssignmentID string `db:"id"`
	Account             string `db:"account"`
	AccountName         string `db:"account_name"`
	PermissionSetARN    string `db:"permission_set_arn"`
	PermissionSetName   string `db:"permission_set_name"`
	GroupName           string `db:"group_name"`
	UserEmail           string `db:"email"`
}

type userAssignment struct {
	AccountAssignmentID string `db:"id"`
	Account             string `db:"account"`
	AccountName         string `db:"account_name"`
	PermissionSetARN    string `db:"permission_set_arn"`
	PermissionSetName   string `db:"permission_set_name"`
	UserEmail           string `db:"email"`
	UserID              string `db:"user_id"`
}

// accessViaCF is an account assignment which is created through Common Fate,
// and shouldn't be removed
type accessViaCF struct {
	UserEmail        string
	PermissionSetARN string
	AccountID        string
}

// Key combines the fields into a single string, so that we can store the
// fields in a map to be looked up against.
func (a accessViaCF) Key() string {
	return fmt.Sprintf("%s+%s+%s", a.UserEmail, a.AccountID, a.PermissionSetARN)
}

var Analyze = cli.Command{
	Name: "analyze",
	Flags: []cli.Flag{
		&cli.PathFlag{Name: "report", Required: true},
		&cli.PathFlag{Name: "requests", Required: true},
		&cli.BoolFlag{Name: "no-dry-run", Usage: "actually remove entitlements"},
	},
	Action: func(c *cli.Context) error {
		_ = godotenv.Load()

		db, err := sqlx.Open("sqlite3", c.Path("report"))
		if err != nil {
			return err
		}

		var groupAssignments []groupAssignment

		clio.Infof("finding AWS SSO entitlements assigned to groups")

		err = db.Select(&groupAssignments, `
SELECT
    accountassignment.id,
    accountassignment.account,
    account.name as account_name,
    accountassignment.permission_set as permission_set_arn,
    permissionset.name as permission_set_name,
	"group".name as group_name,
    user.email
FROM accountassignment
INNER JOIN account ON accountassignment.account = account.id
INNER JOIN permissionset ON accountassignment.permission_set = permissionset.id
INNER JOIN groupmembership ON accountassignment."group" = groupmembership."group"
INNER JOIN user ON groupmembership."user" = user.id
INNER JOIN "group" ON groupmembership."group" = "group".id
		`)
		if err != nil {
			return err
		}

		clio.Debugw("group assignments", "assignments", groupAssignments)

		var userAssignments []userAssignment

		clio.Infof("finding AWS SSO entitlements assigned to users")

		err = db.Select(&userAssignments, `
SELECT
    accountassignment.id,
    accountassignment.account,
    account.name as account_name,
    accountassignment.permission_set as permission_set_arn,
    permissionset.name as permission_set_name,
    user.email,
	user.id as user_id
FROM accountassignment
INNER JOIN account ON accountassignment.account = account.id
INNER JOIN permissionset ON accountassignment.permission_set = permissionset.id
INNER JOIN user ON accountassignment."user" = user.id
		`)
		if err != nil {
			return err
		}

		var describeStr string

		err = db.QueryRow("SELECT describe from __common_fate_meta LIMIT 1").Scan(&describeStr)
		if err != nil {
			return errors.Wrap(err, "querying for provider describe data")
		}

		var describe providerregistrysdk.DescribeResponse
		err = json.Unmarshal([]byte(describeStr), &describe)
		if err != nil {
			return err
		}

		clio.Debugw("user assignments", "assignments", userAssignments)

		var accessRequests []accessRequestWithDetail

		requestsFile := c.Path("requests")
		clio.Infof("loading active Access Requests in Common Fate from %s", requestsFile)

		accessRequestsBytes, err := os.ReadFile(requestsFile)
		if err != nil {
			return err
		}
		err = json.Unmarshal(accessRequestsBytes, &accessRequests)
		if err != nil {
			return err
		}

		// a map of active access requests.
		// These need to be ignored when deprovisioning access, as they are
		// managed by Common Fate.
		commonFateAccessMap := map[string]string{}

		for _, req := range accessRequests {
			if req.Request.AccessRule.Target.Provider.Type != "aws-sso" {
				continue
			}

			ps := req.Request.Arguments.AdditionalProperties["permissionSetArn"].Value
			accountID := req.Request.Arguments.AdditionalProperties["accountId"].Value

			access := accessViaCF{
				UserEmail:        req.User.Email,
				PermissionSetARN: ps,
				AccountID:        accountID,
			}

			commonFateAccessMap[access.Key()] = req.Request.ID
		}

		for _, ga := range groupAssignments {
			// Common Fate only manages individual user access, but log these for informational purposes
			clio.Infof("SKIPPING: user %s has access to %s (%v) with role %s because of group %s - this tool only removes individual user account assignments", ga.UserEmail, ga.AccountName, ga.Account, ga.PermissionSetName, ga.GroupName)
		}

		fmt.Println("#!/bin/bash")

		// look up the config values which will be used to generate the bash script used to remove assignments
		instanceARN := describe.Config["sso_instance_arn"].(string)
		ssoRegion := describe.Config["sso_region"].(string)

		fmt.Printf("SSO_INSTANCE_ARN=%s\n", instanceARN)
		fmt.Printf("SSO_REGION=%s\n\n", ssoRegion)

		for i, ua := range userAssignments {
			access := accessViaCF{
				UserEmail:        ua.UserEmail,
				PermissionSetARN: ua.PermissionSetARN,
				AccountID:        ua.Account,
			}

			key := access.Key()

			if requestID, ok := commonFateAccessMap[key]; ok {
				clio.Infof("SKIPPING: user %s has access to %s (%v) with role %s via Common Fate (Access Request %s) - this account assignment will not be removed", ua.UserEmail, ua.AccountName, ua.Account, ua.PermissionSetName, requestID)
				continue
			}

			// need to remove this account assignment
			clio.Infof("WILL BE REMOVED: user %s has access to %s (%v) with role %s", ua.UserEmail, ua.AccountName, ua.Account, ua.PermissionSetName)
			fmt.Printf("echo \"(%d/%d) removing user %s access to %s (%v) with role %s\"\n", i+1, len(userAssignments), ua.UserEmail, ua.AccountName, ua.Account, ua.PermissionSetName)
			fmt.Printf("aws sso-admin delete-account-assignment --instance-arn $SSO_INSTANCE_ARN --region $SSO_REGION --target-type AWS_ACCOUNT --target-id %s --permission-set-arn %s --principal-type USER --principal-id %s\n\n", ua.Account, ua.PermissionSetARN, ua.UserID)
		}

		return nil
	},
}
