package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/llm"
)

const maxResponseRetryBackoff = time.Duration(1<<63 - 1)

type responseRetryPolicy struct {
	maxRetries int
	backoff    func(int) time.Duration
	sleep      func(context.Context, time.Duration) error
}

func newResponseRetryPolicy(maxRetries int) responseRetryPolicy {
	return responseRetryPolicy{
		maxRetries: maxRetries,
		backoff:    responseRetryBackoff,
		sleep:      sleepWithContext,
	}
}

type responseStreamConsumer func(context.Context, llm.Stream, int) (llm.Response, error)

func (session *ModelClientSession) runResponseStream(
	ctx context.Context,
	request llm.Request,
	events protocol.EventSink,
	consume responseStreamConsumer,
) (llm.Response, error) {
	var lastResponse llm.Response
	for retryCount := 0; ; {
		if err := ctx.Err(); err != nil {
			return lastResponse, err
		}

		attemptCtx, cancelAttempt := context.WithCancel(ctx)
		stream, err := session.client.Stream(attemptCtx, request)
		if err == nil {
			response, consumeErr := consume(attemptCtx, stream, retryCount)
			lastResponse = response
			closeErr := stream.Close()
			cancelAttempt()
			if consumeErr == nil {
				// A completed response is authoritative. A close failure after the
				// terminal chunk must not replay the request and duplicate output.
				return response, nil
			}
			err = errors.Join(consumeErr, closeErr)
		} else {
			cancelAttempt()
		}

		if parentErr := ctx.Err(); parentErr != nil {
			return lastResponse, parentErr
		}
		providerError := normalizeResponseStreamError(err)
		if !providerError.Retryable || retryCount >= session.retry.maxRetries {
			publishErr := publishStreamFailure(context.WithoutCancel(ctx), events, session.client.Model().Provider, providerError, false, providerError.Error())
			return lastResponse, errors.Join(providerError, publishErr)
		}

		retryCount++
		message := fmt.Sprintf("Reconnecting... %d/%d", retryCount, session.retry.maxRetries)
		if publishErr := publishStreamFailure(ctx, events, session.client.Model().Provider, providerError, true, message); publishErr != nil {
			return lastResponse, errors.Join(providerError, publishErr)
		}
		delay := providerError.RetryDelay
		if delay <= 0 {
			delay = session.retry.backoff(retryCount)
		}
		if err := session.retry.sleep(ctx, delay); err != nil {
			return lastResponse, err
		}
	}
}

func normalizeResponseStreamError(err error) *llm.ProviderError {
	if providerError, ok := llm.AsProviderError(err); ok {
		return providerError
	}
	return &llm.ProviderError{
		Kind:              llm.ProviderErrorUnknown,
		Message:           "provider response stream failed",
		AdditionalDetails: llm.SanitizeProviderErrorText(err.Error()),
		Cause:             err,
	}
}

func publishStreamFailure(ctx context.Context, events protocol.EventSink, provider string, providerError *llm.ProviderError, willRetry bool, message string) error {
	if events == nil {
		return errors.New("stream error event sink is nil")
	}
	message = llm.SanitizeProviderErrorText(message)
	details := providerError.AdditionalDetails
	if details == "" {
		details = providerError.Message
	}
	details = llm.SanitizeProviderErrorText(details)
	var detailPointer *string
	if details != "" {
		detailPointer = &details
	}
	return events.Publish(ctx, protocol.Event{Msg: protocol.StreamErrorEvent{
		Message:           message,
		AdditionalDetails: detailPointer,
		ProviderError: &protocol.ProviderErrorInfo{
			Kind:       string(providerError.Kind),
			Code:       providerError.Code,
			StatusCode: providerError.StatusCode,
			RequestID:  providerError.RequestID,
			Provider:   provider,
			Retryable:  providerError.Retryable,
			RetryDelay: providerError.RetryDelay,
		},
		WillRetry: willRetry,
	}})
}

func responseRetryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	base := math.Ldexp(float64(200*time.Millisecond), attempt-1)
	jitter := 0.9 + rand.Float64()*0.2
	delay := base * jitter
	if math.IsInf(delay, 0) || delay >= float64(maxResponseRetryBackoff) {
		return maxResponseRetryBackoff
	}
	return time.Duration(delay)
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
