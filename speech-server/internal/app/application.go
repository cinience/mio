package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	openairt "github.com/WqyJh/go-openai-realtime/v2"

	"speech-server/internal/asr"
	"speech-server/internal/config"
	"speech-server/internal/logger"
	"speech-server/internal/middleware"
	"speech-server/internal/realtime"
	"speech-server/internal/speaker"
	"speech-server/internal/tts"
)

type Application struct {
	cfg          config.Config
	httpServer   *http.Server
	realtime     *realtime.Server
	voiceMap     map[openairt.Voice]int
	defaultVoice openairt.Voice
	recog        *asr.Recognizer
	tts          *tts.Generator
	normalizer   *tts.TtsNormalizer
	vadFactory   *asr.VADFactory
	vadPool      *asr.VADPool
	speaker      *speaker.Manager
	speakerCfg   speaker.Config
}

var (
	startTime    = time.Now()
	buildVersion = "0.0.0-dev"
)

type healthResponse struct {
	Status  string            `json:"status"`
	Checks  map[string]string `json:"checks"`
	Version string            `json:"version"`
	Uptime  string            `json:"uptime,omitempty"`
}

func NewApplication(cfg config.Config) (*Application, error) {
	if err := config.MaybeBootstrapModels(&cfg); err != nil {
		return nil, err
	}

	model, ok := cfg.ASRModels[cfg.ASRProvider]
	if !ok {
		return nil, fmt.Errorf("asr provider %q not found", cfg.ASRProvider)
	}

	recog, err := asr.NewRecognizer(model)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize ASR recognizer: %w", err)
	}

	var (
		ttsGen       *tts.Generator
		voiceMap     map[openairt.Voice]int
		voice                = realtime.NormalizeVoice(openairt.VoiceAlloy)
		defaultSpeed float32 = 1.0
		normalizer   *tts.TtsNormalizer
		vadFactory   *asr.VADFactory
		vadPool      *asr.VADPool
	)

	if cfg.TTSProvider != "" {
		modelCfg, ok := cfg.TTSModels[cfg.TTSProvider]
		if !ok {
			recog.Close()
			return nil, fmt.Errorf("tts provider %q not found", cfg.TTSProvider)
		}
		gen, err := tts.NewGenerator(modelCfg.Config)
		if err != nil {
			recog.Close()
			return nil, fmt.Errorf("failed to initialize TTS generator: %w", err)
		}
		ttsGen = gen
		voiceMap = modelCfg.VoiceMap
		voice = modelCfg.DefaultVoice
		defaultSpeed = modelCfg.DefaultSpeed
		if modelCfg.Config.EnableCN2AN {
			normalizer = tts.NewTTSNormalizer()
		}
		logger.Infof("tts model %s initialized", cfg.TTSProvider)
	}

	if voice == "" {
		voice = realtime.NormalizeVoice(openairt.VoiceAlloy)
	}
	if len(voiceMap) == 0 {
		voiceMap = map[openairt.Voice]int{
			voice: 0,
		}
	}

	if cfg.VADEnabled && len(cfg.VADModels) > 0 {
		factory, err := asr.NewVADFactory(cfg.VADModels)
		if err != nil {
			if ttsGen != nil {
				ttsGen.Close()
			}
			recog.Close()
			return nil, fmt.Errorf("failed to initialise VAD factory: %w", err)
		}
		vadFactory = factory
		pool, err := asr.NewVADPool(asr.VADPoolOption{
			Factory:  factory,
			Provider: cfg.VADProvider,
			MinSize:  cfg.VADPool.MinSize,
			MaxSize:  cfg.VADPool.MaxSize,
			Timeout:  cfg.VADPool.AcquireTimeout,
		})
		if err != nil {
			if ttsGen != nil {
				ttsGen.Close()
			}
			recog.Close()
			return nil, fmt.Errorf("failed to initialise VAD pool: %w", err)
		}
		vadPool = pool
		logger.Infof("vad factory initialised with provider %s", cfg.VADProvider)
	}

	var (
		speakerMgr *speaker.Manager
		speakerCfg speaker.Config
	)
	if cfg.Speaker.Enabled {
		speakerCfg = speaker.Config{
			Enabled:         cfg.Speaker.Enabled,
			ModelPath:       cfg.Speaker.ModelPath,
			SampleRate:      cfg.Speaker.SampleRate,
			NumThreads:      cfg.Speaker.NumThreads,
			Provider:        cfg.Speaker.Provider,
			Threshold:       cfg.Speaker.Threshold,
			MinDuration:     cfg.Speaker.MinDuration,
			EnergyThreshold: cfg.Speaker.EnergyThreshold,
			DataDir:         cfg.Speaker.DataDir,
			VectorDB: speaker.VectorDBConfig{
				Provider: cfg.Speaker.VectorDB.Provider,
				Chromem: speaker.ChromemConfig{
					Path:     cfg.Speaker.VectorDB.Chromem.Path,
					Compress: cfg.Speaker.VectorDB.Chromem.Compress,
				},
			},
		}
		mgr, err := speaker.NewManager(speakerCfg)
		if err != nil {
			logger.Warnf("speaker init failed, disabling: %v", err)
		} else {
			speakerMgr = mgr
			logger.Infof("speaker module initialised (provider=%s)", speakerCfg.VectorDB.Provider)
		}
	}

	defaultClientRate := model.SampleRate
	if defaultClientRate <= 0 {
		defaultClientRate = recog.SampleRate()
	}

	rtServer, err := realtime.NewServer(realtime.Options{
		Recognizer:           recog,
		DefaultClientRate:    defaultClientRate,
		TTS:                  ttsGen,
		VoiceMap:             voiceMap,
		DefaultVoice:         voice,
		DefaultTTSSpeed:      defaultSpeed,
		Normalizer:           normalizer,
		VADEnabled:           cfg.VADEnabled,
		VADProvider:          cfg.VADProvider,
		VADFactory:           vadFactory,
		VADPool:              vadPool,
		AudioEnergyThreshold: cfg.AudioEnergyThreshold,
		AudioMinDuration:     cfg.AudioMinDuration,
		Speaker:              speakerMgr,
		SpeakerConfig:        speakerCfg,
	})
	if err != nil {
		if ttsGen != nil {
			ttsGen.Close()
		}
		if vadPool != nil {
			vadPool.Close()
		}
		recog.Close()
		return nil, err
	}

	app := &Application{
		cfg:          cfg,
		realtime:     rtServer,
		voiceMap:     voiceMap,
		defaultVoice: voice,
		recog:        recog,
		tts:          ttsGen,
		normalizer:   normalizer,
		vadFactory:   vadFactory,
		vadPool:      vadPool,
		speaker:      speakerMgr,
		speakerCfg:   speakerCfg,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/realtime", rtServer.Handle)
	mux.HandleFunc("/health", app.handleHealth)
	if speakerMgr != nil {
		h := speaker.NewHandler(speakerMgr, speakerCfg)
		h.RegisterRoutes(mux)
	}
	app.registerWebUIRoutes(mux)

	handler := middleware.Recovery(mux)

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: handler,
	}

	app.httpServer = srv

	return app, nil
}

func (a *Application) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return a.runWithContext(ctx)
}

// RunContext starts the HTTP server and blocks until the provided context is
// cancelled or the server exits.
func (a *Application) RunContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("nil context provided")
	}
	return a.runWithContext(ctx)
}

func (a *Application) runWithContext(ctx context.Context) error {
	defer a.Close()

	errCh := make(chan error, 1)
	go func() {
		logger.Infof("speech-server listening on %s", a.cfg.Addr)
		err := a.httpServer.ListenAndServe()
		if err == http.ErrServerClosed {
			err = nil
		} else if err != nil {
			err = fmt.Errorf("server error: %w", err)
		}
		errCh <- err
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdownErr := a.httpServer.Shutdown(shutdownCtx)
		serverErr := <-errCh
		if shutdownErr != nil {
			if serverErr != nil {
				return fmt.Errorf("server error: %v; shutdown error: %w", serverErr, shutdownErr)
			}
			return fmt.Errorf("server shutdown error: %w", shutdownErr)
		}
		return serverErr
	}
}

func (a *Application) Close() {
	logger.Infof("shutting down speech-server...")
	if a.vadPool != nil {
		logger.Infof("closing VAD pool...")
		a.vadPool.Close()
		a.vadPool = nil
	}
	if a.tts != nil {
		logger.Infof("closing TTS generator...")
		a.tts.Close()
		a.tts = nil
	}
	if a.recog != nil {
		logger.Infof("closing ASR recognizer...")
		a.recog.Close()
		a.recog = nil
	}
	if a.speaker != nil {
		logger.Infof("closing speaker manager...")
		a.speaker.Close()
		a.speaker = nil
	}
	logger.Infof("speech-server shutdown complete")
}

func (a *Application) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := "healthy"
	checks := map[string]string{}

	if a.recog != nil {
		checks["asr"] = "healthy"
	} else {
		checks["asr"] = "unavailable"
		status = "degraded"
	}

	if a.tts != nil {
		checks["tts"] = "healthy"
	} else {
		checks["tts"] = "disabled"
	}

	if a.speaker != nil {
		checks["speaker"] = "healthy"
	} else if a.cfg.Speaker.Enabled {
		checks["speaker"] = "degraded"
		status = "degraded"
	} else {
		checks["speaker"] = "disabled"
	}

	if a.vadPool != nil {
		checks["vad"] = "healthy"
	} else {
		checks["vad"] = "disabled"
	}

	resp := healthResponse{
		Status:  status,
		Checks:  checks,
		Version: getVersion(),
		Uptime:  time.Since(startTime).String(),
	}

	w.Header().Set("Content-Type", "application/json")
	if status != "healthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Errorf("health check encode error: %v", err)
	}
}

func getVersion() string {
	if buildVersion == "" {
		return "0.0.0-dev"
	}
	return buildVersion
}

func ParseFlags() (config.Config, error) {
	return config.ParseFlags()
}
