package errors

import (
	"context"

	"backend-server/internal/server/observability"
)

// ErrorClassifier allows custom classification logic for arbitrary error types.
type ErrorClassifier func(error) *AppError

// ErrorHandlerFunc processes a classified error and may return a replacement error.
type ErrorHandlerFunc func(context.Context, *AppError) error

// Handler routes errors through classifiers, metrics, and handler callbacks.
type Handler struct {
	classifiers []ErrorClassifier
	handlers    map[ErrorType]ErrorHandlerFunc
	metrics     *observability.ServerMetrics
}

// NewHandler constructs an error handler bound to the provided metrics collector.
func NewHandler(metrics *observability.ServerMetrics) *Handler {
	return &Handler{
		classifiers: make([]ErrorClassifier, 0),
		handlers:    make(map[ErrorType]ErrorHandlerFunc),
		metrics:     metrics,
	}
}

// RegisterClassifier appends a classifier to the evaluation chain.
func (h *Handler) RegisterClassifier(classifier ErrorClassifier) {
	if h == nil || classifier == nil {
		return
	}
	h.classifiers = append(h.classifiers, classifier)
}

// RegisterHandler associates a handler function with a specific error type.
func (h *Handler) RegisterHandler(t ErrorType, fn ErrorHandlerFunc) {
	if h == nil || fn == nil {
		return
	}
	h.handlers[t] = fn
}

// Handle classifies, records, and dispatches the provided error.
func (h *Handler) Handle(ctx context.Context, err error) *AppError {
	if err == nil || h == nil {
		return nil
	}

	appErr := Classify(err)
	for _, classifier := range h.classifiers {
		if classifier == nil {
			continue
		}
		if candidate := classifier(err); candidate != nil {
			appErr = candidate
			break
		}
	}

	if appErr == nil {
		return nil
	}

	if h.metrics != nil {
		h.metrics.RecordError(string(appErr.Type), string(appErr.Severity))
	}

	if handler, ok := h.handlers[appErr.Type]; ok && handler != nil {
		if handlerErr := handler(ctx, appErr); handlerErr != nil {
			return Classify(handlerErr)
		}
	}

	return appErr
}
