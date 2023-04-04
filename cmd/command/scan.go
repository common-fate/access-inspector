package command

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/common-fate/access-inspector/pkg/loader"
	"github.com/common-fate/clio"
	"github.com/common-fate/provider-registry-sdk-go/pkg/handlerclient"
	"github.com/common-fate/provider-registry-sdk-go/pkg/providerregistrysdk"
	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

// tableName gets the SQlite table name for a particular resource type
func tableName(p providerregistrysdk.Provider, schemaVersion string, resourceType string) string {
	return strings.ToLower(resourceType)
}

var Scan = cli.Command{
	Name: "scan",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "provider-local-path", Required: true},
	},
	Action: func(c *cli.Context) error {
		ctx := c.Context
		_ = godotenv.Load()

		hc := handlerclient.Client{
			Executor: handlerclient.Local{
				Dir: c.String("provider-local-path"),
			},
		}

		describe, err := hc.Describe(ctx)
		if err != nil {
			return err
		}

		var tasks []string
		for task := range describe.Schema.Resources.Loaders {
			tasks = append(tasks, task)
		}

		now := time.Now().UTC()
		dbfilename := fmt.Sprintf("file:reports/%s.db", now.Format("2006-01-02T15-04-05"))

		db, err := sqlx.Open("sqlite3", dbfilename)
		if err != nil {
			return err
		}

		// TODO: don't hardcode
		// (local providers don't return version details at the moment when Describe is called
		// so need to hardcode for now)
		p := providerregistrysdk.Provider{
			Name:      "aws",
			Publisher: "common_fate",
			Version:   "v0.4.0",
		}
		schemaVersion := "v1" // hardcode for now

		for resourceType, resource := range describe.Schema.Resources.Types {

			table := tableName(p, schemaVersion, resourceType)

			resourceData := resource.(map[string]any)
			properties := resourceData["properties"].(map[string]any)

			cols := []string{
				`"id" TEXT PRIMARY KEY`,
				`"name" TEXT`,
			}

			if _, ok := properties["data"]; ok {
				dataProps := properties["data"].(map[string]any)

				for property := range dataProps {
					// default to TEXT columns for everything for now
					col := fmt.Sprintf(`"%s" TEXT`, property)
					cols = append(cols, col)
				}
			}

			stmt := fmt.Sprintf(`CREATE TABLE "%s" (%s)`, table, strings.Join(cols, ", "))

			clio.Debugw("creating table", "sql", stmt)

			_, err = db.Exec(stmt)
			if err != nil {
				return errors.Wrapf(err, "creating table %s", table)
			}
		}

		fetcher := loader.NewResourceFetcher(&hc)

		clio.Infow("loading resources", "tasks", tasks)

		resources, err := fetcher.LoadResources(ctx, tasks)
		if err != nil {
			return err
		}

		for _, r := range resources {
			table := tableName(p, schemaVersion, r.Type)

			cols := []string{`"id"`, `"name"`}
			vals := []string{`'` + r.ID + `'`, `'` + r.Name + `'`}

			// add the data fields to the cols/vals
			// so that they can be inserted into the database

			for k, v := range r.Data {
				if v == nil {
					continue
				}

				cols = append(cols, `"`+k+`"`)

				var valString string
				switch val := v.(type) {
				case string:
					valString = `'` + val + `'`
				default:
					// JSON encode the value if it's not a string
					valBytes, err := json.Marshal(val)
					if err != nil {
						return err
					}
					valString = `'` + string(valBytes) + `'`
				}

				vals = append(vals, valString)
			}

			stmt := fmt.Sprintf(`INSERT INTO "%s" (%s) VALUES (%s)`, table, strings.Join(cols, ", "), strings.Join(vals, ", "))

			clio.Debugw("inserting data", "sql", stmt)

			_, err = db.Exec(stmt)
			if err != nil {
				return errors.Wrapf(err, "inserting %+v into database", r)
			}
		}

		// clio.Infow("got resources", resources)

		return nil
	},
}
