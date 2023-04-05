package command

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/common-fate/cli/pkg/client"
	"github.com/common-fate/cli/pkg/config"
	"github.com/common-fate/clio"
	"github.com/common-fate/common-fate/pkg/types"
	_ "github.com/mattn/go-sqlite3"
	"github.com/urfave/cli/v2"
	"golang.org/x/sync/errgroup"
)

type accessRequestWithDetail struct {
	Request types.RequestDetail `json:"request"`
	User    types.User          `json:"user"`
}

var DumpRequests = cli.Command{
	Name: "dump-requests",
	Flags: []cli.Flag{
		&cli.PathFlag{Name: "output", Required: true},
	},
	Action: func(c *cli.Context) error {
		ctx := c.Context
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		cf, err := client.FromConfig(ctx, cfg)
		if err != nil {
			return err
		}

		var accessRequests []types.Request

		var done bool
		var nextToken *string
		var i int

		approved := types.AdminListRequestsParamsStatus("APPROVED")

		for !done {
			i++
			clio.Infof("finding active Access Requests in Common Fate (page %d)", i)

			res, err := cf.AdminListRequestsWithResponse(ctx, &types.AdminListRequestsParams{Status: &approved, NextToken: nextToken})
			if err != nil {
				return err
			}

			now := time.Now()

			for _, req := range res.JSON200.Requests {
				if req.Grant != nil &&
					req.Grant.Status == types.GrantStatusACTIVE &&
					req.Grant.Start.Before(now) && req.Grant.End.After(now) {
					accessRequests = append(accessRequests, req)
				}
			}

			if res.JSON200.Next == nil {
				done = true
			}

			nextToken = res.JSON200.Next
		}

		var mu sync.Mutex
		var accessRequestsWithDetail []accessRequestWithDetail
		var g errgroup.Group

		for _, req := range accessRequests {
			reqID := req.ID

			g.Go(func() error {
				res, err := cf.AdminGetRequestWithResponse(ctx, reqID)
				if err != nil {
					return err
				}
				user, err := cf.UserGetUserWithResponse(ctx, res.JSON200.Requestor)
				if err != nil {
					return err
				}
				mu.Lock()
				defer mu.Unlock()
				detail := accessRequestWithDetail{
					Request: *res.JSON200,
					User:    *user.JSON200,
				}
				accessRequestsWithDetail = append(accessRequestsWithDetail, detail)
				return nil
			})
		}

		err = g.Wait()
		if err != nil {
			return err
		}

		clio.Debugw("found active Access Requests", "requests", accessRequestsWithDetail)

		requestsBytes, err := json.Marshal(accessRequestsWithDetail)
		if err != nil {
			return err
		}

		output := c.String("output")
		err = os.WriteFile(output, requestsBytes, 0755)
		if err != nil {
			return err
		}

		clio.Successf("wrote active Access Requests to %s", output)

		return nil
	},
}
