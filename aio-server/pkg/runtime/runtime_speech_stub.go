//go:build !sherpa_onnx

package runtime

import "context"

func (r *Runtime) startSpeech(ctx context.Context) error {
	return nil
}

func (r *Runtime) stopSpeech(ctx context.Context) error {
	return nil
}
