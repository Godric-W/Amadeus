package app

import "context"

func (application *InteractiveApplication) Shutdown(ctx context.Context) error {
	if application == nil {
		return nil
	}
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	err := application.workspace.Close(ctx)
	application.clearAttachmentForShutdown()
	return err
}

func (application *InteractiveApplication) Close() {
	if application == nil {
		return
	}
	application.clearAttachmentForShutdown()
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
