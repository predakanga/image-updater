package pkg

import (
	"context"
	"errors"
	"fmt"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
	"github.com/cenkalti/backoff/v4"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
)

const argoTimeout = 300

func (s *WebhookServer) argoSync(applicationName string, waitForRevision string) {
	// Set up a context so that we don't retry forever
	ctx, cancel := context.WithTimeout(context.Background(), argoTimeout*time.Second)
	defer cancel()
	// Retry with exponential backoff, in case the argo server is unavailable
	err := backoff.Retry(func() error {
		return s.doArgoSync(ctx, applicationName, waitForRevision)
	}, backoff.WithContext(backoff.NewExponentialBackOff(), ctx))
	if err != nil {
		logFields := map[string]interface{}{
			"application": applicationName,
			"revision":    waitForRevision,
		}
		if errors.Is(err, context.DeadlineExceeded) {
			log.WithFields(logFields).Warn("Timed out waiting for ArgoCD sync")
		} else {
			log.WithError(err).WithFields(logFields).Warn("Could not trigger ArgoCD sync")
		}
	}
}

func (s *WebhookServer) doArgoSync(ctx context.Context, applicationName string, waitForRevision string) error {
	logFields := map[string]interface{}{
		"application": applicationName,
		"revision":    waitForRevision,
	}
	// Open a connection to the ArgoCD server
	client, err := apiclient.NewClient(&apiclient.ClientOptions{
		ServerAddr: s.argoUrl,
		AuthToken:  s.argoToken,
	})
	if err != nil {
		return fmt.Errorf("connecting to argocd failed: %w", err)
	}
	closer, appClient, err := client.NewApplicationClient()
	if err != nil {
		return fmt.Errorf("creating application client failed: %w", err)
	}
	defer closer.Close()
	// Fetch the application to make sure we're authenticated
	if _, err := appClient.Get(ctx, &application.ApplicationQuery{Name: &applicationName}); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return backoff.Permanent(err)
		}
		if errStatus, ok := status.FromError(err); ok {
			if errStatus.Code() == codes.Unauthenticated || errStatus.Code() == codes.PermissionDenied {
				return backoff.Permanent(err)
			}
		}
		return err
	}
	// Wait for ArgoCD to notify us that the revision is available
	revChan := client.WatchApplicationWithRetry(ctx, applicationName, "")
	ready := false
	for !ready {
		select {
		case event := <-revChan:
			log.WithFields(logFields).Debugf("Application revision is now %s", event.Application.Status.Sync.Revision)
			// TODO: Check whether Revisions always includes Revision
			if event.Application.Status.Sync.Revision == waitForRevision {
				ready = true
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	// Finally, trigger the synchronization
	if _, err := appClient.Sync(ctx, &application.ApplicationSyncRequest{Name: &applicationName}); err != nil {
		return fmt.Errorf("synchronizing application failed: %w", err)
	}
	log.WithFields(logFields).Info("Application synchronized")

	return nil
}
