package command

import (
	"bytes"
	"fmt"
	"os"

	"github.com/common-fate/access-inspector/pkg/loader"
	"github.com/common-fate/clio"
	"github.com/common-fate/provider-registry-sdk-go/pkg/handlerclient"
	"github.com/common-fate/provider-registry-sdk-go/pkg/msg"
	"github.com/dominikbraun/graph"
	"github.com/dominikbraun/graph/draw"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

var Hash = func(n msg.Resource) string {
	return n.Type + "/" + n.ID
}

type Report struct {
	G graph.Graph[string, msg.Resource]
}

func resourceLabel(r msg.Resource) string {
	if r.Name == "" {
		return r.Type + "/" + r.ID
	}
	return r.Type + "/" + r.Name
}

var AnalyzeGraph = cli.Command{
	Name: "analyze",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "report", Required: true},
	},
	Action: func(c *cli.Context) error {
		ctx := c.Context
		_ = godotenv.Load()

		hc := handlerclient.Client{
			Executor: handlerclient.Local{
				Dir: c.String("report"),
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

		fetcher := loader.NewResourceFetcher(&hc)

		clio.Infow("loading resources", "tasks", tasks)

		resources, err := fetcher.LoadResources(ctx, tasks)
		if err != nil {
			return err
		}

		report := Report{G: graph.New(Hash)}

		var users []string

		for _, r := range resources {
			clio.Debugw("adding node", "resource", r)
			label := resourceLabel(r)
			err = report.G.AddVertex(r, graph.VertexAttribute("label", label))
			// ok if edge already exists as it would have been inserted from a related field
			if err == graph.ErrVertexAlreadyExists {
				_, props, err := report.G.VertexWithProperties(Hash(r))
				if err != nil {
					return err
				}
				// ensure the label is set
				props.Attributes["label"] = label
			} else if err != nil {
				return err
			}

			resourceSchema := describe.Schema.Resources.Types[r.Type]
			resourceSchemaMap := resourceSchema.(map[string]any)
			resourceProps := resourceSchemaMap["properties"].(map[string]any)

			// if the type is User, add it to the list of users to analyse
			if r.Type == "User" {
				users = append(users, Hash(r))
			}

			for k, v := range r.Data {
				if v == nil {
					continue
				}

				resourcePropsData := resourceProps["data"].(map[string]any)
				schema := resourcePropsData[k].(map[string]any)
				relation, ok := schema["relation"]
				if !ok {
					continue
				}

				relationStr := relation.(string)

				vStr, ok := v.(string)
				if !ok {
					return fmt.Errorf("could not cast field %s to string (%+v)", k, v)
				}

				// create an edge to the related field
				to := msg.Resource{
					Type: relationStr,
					ID:   vStr,
				}

				err = report.G.AddVertex(to)
				if err != nil && err != graph.ErrVertexAlreadyExists {
					return errors.Wrap(err, "adding connected resource vertex")
				}

				err = report.G.AddEdge(Hash(r), Hash(to))
				if err != nil {
					return err
				}
			}
		}

		for _, u := range users {
			clio.Infow("analysing access", "user", u)
			graph.BFS(report.G, u, func(k string) bool {
				return false
			})

		}

		var b bytes.Buffer

		draw.DOT(report.G, &b)

		os.WriteFile("result", b.Bytes(), 0755)

		return nil
	},
}
