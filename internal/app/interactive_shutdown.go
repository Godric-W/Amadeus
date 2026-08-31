package app

import (
	"context"
	"errors"
	"time"
)

func (application *InteractiveApplication) Shutdown(ctx context.Context) error {
	if application == nil {
		return nil
	}
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	err := application.workspace.Close(ctx)
	application.clearAttachmentForShutdown()
	return errors.Join(err, application.waitForAttachmentPumps(ctx), application.waitForReleases(ctx))
}

func (application *InteractiveApplication) Close() {
	if application == nil {
		return
	}
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	application.clearAttachmentForShutdown()
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(application.ctx), 5*time.Second)
	defer cancel()
	_ = application.waitForAttachmentPumps(cleanupCtx)
	_ = application.waitForReleases(cleanupCtx)
	application.cancel()
}

func (application *InteractiveApplication) clearAttachmentForShutdown() {
	application.mu.Lock()
	if application.attachmentCancel != nil {
		application.attachmentCancel()
		application.attachmentCancel = nil
	}
	application.active = nil
	application.phase = "shutdown"
	application.mu.Unlock()
}
