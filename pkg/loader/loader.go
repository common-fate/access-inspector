package loader

import (
	"context"
	"errors"
	"os/exec"
	"path"
	"sync"

	"github.com/common-fate/apikit/logger"
	"github.com/common-fate/clio"
	"github.com/common-fate/provider-registry-sdk-go/pkg/handlerclient"
	"github.com/common-fate/provider-registry-sdk-go/pkg/msg"
	"golang.org/x/sync/errgroup"
)

// ResourceFetcher fetches resources from provider lambda handler based on
// provider schema's "loadResources" object.
type ResourceFetcher struct {
	resourcesMx sync.Mutex
	// This map stores and deduplicates returned resources
	resources map[string]msg.Resource
	eg        *errgroup.Group
	runtime   *handlerclient.Client
}

func NewResourceFetcher(runtime *handlerclient.Client) *ResourceFetcher {
	return &ResourceFetcher{
		runtime:   runtime,
		resources: make(map[string]msg.Resource),
	}
}

// LoadResources invokes the deployment
func (rf *ResourceFetcher) LoadResources(ctx context.Context, tasks []string) (map[string]msg.Resource, error) {

	// reset any loaded resources to prevent duplicates
	rf.resources = map[string]msg.Resource{}

	eg, gctx := errgroup.WithContext(ctx)
	rf.eg = eg
	for _, task := range tasks {
		// copy the loop variable
		tc := task
		rf.eg.Go(func() error {
			// Initializing empty context for initial lambda invocation as context
			// as context value for first invocation is irrelevant.
			response, err := rf.runtime.FetchResources(gctx, msg.LoadResources{Task: tc, Ctx: map[string]any{}})
			if err != nil {
				var ee *exec.ExitError
				if errors.As(err, &ee) {
					logger.Get(gctx).Errorw("failed to invoke local python code", "stderr", string(ee.Stderr))
				}
				return err
			}

			return rf.getResources(gctx, *response)
		})
	}

	err := rf.eg.Wait()
	if err != nil {
		return nil, err
	}

	return rf.resources, nil
}

// resourceKey returns a unique identifier for the resource in the format <name>/<id>
func resourceKey(r msg.Resource) string {
	return path.Join(r.Type, r.ID)
}

// Recursively call the provider lambda handler unless there is no further pending tasks.
// the response Resource is then appended to `rf.resources` for batch DB update later on.
func (rf *ResourceFetcher) getResources(ctx context.Context, response msg.LoadResponse) error {

	rf.resourcesMx.Lock()
	for _, r := range response.Resources {
		key := resourceKey(r)
		rf.resources[key] = r
		clio.Infow("found", "resource", r)
	}
	rf.resourcesMx.Unlock()

	for _, task := range response.Tasks {
		// copy the loop variable
		tc := task
		rf.eg.Go(func() error {
			response, err := rf.runtime.FetchResources(ctx, msg.LoadResources(tc))
			if err != nil {
				var ee *exec.ExitError
				if errors.As(err, &ee) {
					logger.Get(ctx).Errorw("failed to invoke local python code", "stderr", string(ee.Stderr))
				}
				return err
			}
			return rf.getResources(ctx, *response)
		})
	}
	return nil
}
