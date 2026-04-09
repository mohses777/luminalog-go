package luminalog

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	DefaultEndpoint      = "https://api.luminalog.cloud/v1/logs"
	DefaultBatchSize     = 100
	MinBatchSize         = 1
	MaxBatchSize         = 500
	DefaultFlushInterval = 5 * time.Second
)

type LogLevel string

const (
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
	LevelFatal LogLevel = "fatal"
	LevelPanic LogLevel = "panic"
)

type ErrorPayload struct {
	Type        string                 `json:"type"`
	Message     string                 `json:"message"`
	StackTrace  []string               `json:"stack_trace,omitempty"`
	Fingerprint string                 `json:"fingerprint,omitempty"`
	Context     map[string]interface{} `json:"context,omitempty"`
}

type LogEntry struct {
	Timestamp   string                 `json:"timestamp"`
	Level       LogLevel               `json:"level"`
	Message     string                 `json:"message"`
	Environment string                 `json:"environment"`
	ProjectID   string                 `json:"project_id,omitempty"`
	PrivacyMode bool                   `json:"privacy_mode,omitempty"`
	Error       *ErrorPayload          `json:"error,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

type LogBatch struct {
	APIKey string     `json:"api_key"`
	Logs   []LogEntry `json:"logs"`
}

type IngestionResponse struct {
	Message     string `json:"message"`
	Processed   int    `json:"processed"`
	DebugUserID string `json:"debug_user_id,omitempty"`
}

// Config holds the configuration for the LuminaLog SDK
type Config struct {
	APIKey        string        // Your LuminaLog API key (required)
	Environment   string        // Label for filtering logs (stored as metadata). Actual environment is set by your API key.
	ProjectID     string        // Optional project identifier
	PrivacyMode   bool          // Enable privacy mode to skip PII scrubbing
	MinLevel      *LogLevel     // Minimum log level to send
	BatchSize     int           // Number of logs before auto-flush (default: 100)
	FlushInterval time.Duration // Time between auto-flushes (default: 5s)
	Endpoint      string        // API endpoint URL (default: LuminaLog cloud)
	Debug         bool          // Enable debug logging
}

type Logger struct {
	apiKey        string
	environment   string
	projectID     string
	privacyMode   bool
	minLevel      *LogLevel
	batchSize     int
	flushInterval time.Duration
	endpoint      string
	debug         bool
	baseMetadata  map[string]interface{}

	queue       []LogEntry
	queueMu     sync.Mutex
	isFlushing  bool
	flushTicker *time.Ticker
	stopChan    chan struct{}
	httpClient  *http.Client
	shutdown    bool
	logLevels   []LogLevel
}

// GenerateTraceID creates a UUID v4 string for trace correlation.
func GenerateTraceID() string {
	return generateUUIDv4()
}

// GenerateSpanID creates a UUID v4 string for span correlation.
func GenerateSpanID() string {
	return generateUUIDv4()
}

// GetTraceIDFromRequest extracts a trace ID from request headers.
// Priority: x-trace-id, x-request-id, then W3C traceparent.
func GetTraceIDFromRequest(req *http.Request) string {
	if req != nil {
		if traceID := req.Header.Get("x-trace-id"); traceID != "" {
			return traceID
		}
		if requestID := req.Header.Get("x-request-id"); requestID != "" {
			return requestID
		}

		if traceparent := req.Header.Get("traceparent"); traceparent != "" {
			parts := strings.Split(traceparent, "-")
			if len(parts) >= 2 && parts[1] != "" {
				return parts[1]
			}
		}
	}

	return GenerateTraceID()
}

func generateUUIDv4() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based deterministic shape if RNG fails.
		now := time.Now().UnixNano()
		return fmt.Sprintf("%08x-%04x-4%03x-a%03x-%012x",
			uint32(now), uint16(now>>32), uint16(now>>16)&0x0fff, uint16(now)&0x0fff, uint64(now))
	}

	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uint32(b[0])<<24|uint32(b[1])<<16|uint32(b[2])<<8|uint32(b[3]),
		uint16(b[4])<<8|uint16(b[5]),
		uint16(b[6])<<8|uint16(b[7]),
		uint16(b[8])<<8|uint16(b[9]),
		uint64(b[10])<<40|uint64(b[11])<<32|uint64(b[12])<<24|uint64(b[13])<<16|uint64(b[14])<<8|uint64(b[15]),
	)
}

func New(config Config) (*Logger, error) {
	if config.APIKey == "" {
		return nil, errors.New("luminalog: api_key is required")
	}

	if config.Environment == "" {
		config.Environment = "default"
	}
	if config.BatchSize == 0 {
		config.BatchSize = DefaultBatchSize
	}

	// Enforce batch size limits
	if config.BatchSize < MinBatchSize {
		fmt.Printf("[LuminaLog] Warning: batchSize %d is below minimum %d. Using %d instead to optimize costs.\n",
			config.BatchSize, MinBatchSize, MinBatchSize)
		config.BatchSize = MinBatchSize
	} else if config.BatchSize > MaxBatchSize {
		fmt.Printf("[LuminaLog] Warning: batchSize %d exceeds maximum %d. Using %d instead.\n",
			config.BatchSize, MaxBatchSize, MaxBatchSize)
		config.BatchSize = MaxBatchSize
	}

	if config.FlushInterval == 0 {
		config.FlushInterval = DefaultFlushInterval
	}
	if config.Endpoint == "" {
		config.Endpoint = DefaultEndpoint
	}

	logger := &Logger{
		apiKey:        config.APIKey,
		environment:   config.Environment,
		projectID:     config.ProjectID,
		privacyMode:   config.PrivacyMode,
		minLevel:      config.MinLevel,
		batchSize:     config.BatchSize,
		flushInterval: config.FlushInterval,
		endpoint:      config.Endpoint,
		debug:         config.Debug,
		baseMetadata:  make(map[string]interface{}),
		queue:         make([]LogEntry, 0, config.BatchSize),
		stopChan:      make(chan struct{}),
		logLevels:     []LogLevel{LevelDebug, LevelInfo, LevelWarn, LevelError, LevelFatal, LevelPanic},
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	logger.startFlushTimer()
	logger.setupShutdownHooks()

	logger.Debug("LuminaLog SDK initialized", map[string]interface{}{
		"environment": logger.environment,
	})

	return logger, nil
}

func (l *Logger) Debug(message string, metadata map[string]interface{}) {
	l.log(LevelDebug, message, metadata)
}

func (l *Logger) Info(message string, metadata map[string]interface{}) {
	l.log(LevelInfo, message, metadata)
}

func (l *Logger) Warn(message string, metadata map[string]interface{}) {
	l.log(LevelWarn, message, metadata)
}

func (l *Logger) Error(message string, metadata map[string]interface{}) {
	l.log(LevelError, message, metadata)
}

func (l *Logger) Fatal(message string, metadata map[string]interface{}) {
	l.log(LevelFatal, message, metadata)
}

func (l *Logger) Panic(message string, metadata map[string]interface{}) {
	entry := l.createEntry(LevelPanic, message, metadata)
	l.queueMu.Lock()
	l.queue = append(l.queue, entry)
	l.queueMu.Unlock()
	l.Flush()
}

func (l *Logger) Child(metadata map[string]interface{}) *Logger {
	childLogger := &Logger{
		apiKey:        l.apiKey,
		environment:   l.environment,
		projectID:     l.projectID,
		privacyMode:   l.privacyMode,
		minLevel:      l.minLevel,
		batchSize:     l.batchSize,
		flushInterval: l.flushInterval,
		endpoint:      l.endpoint,
		debug:         l.debug,
		baseMetadata:  make(map[string]interface{}),
		queue:         make([]LogEntry, 0, l.batchSize),
		stopChan:      make(chan struct{}),
		logLevels:     l.logLevels,
		httpClient:    l.httpClient,
	}

	for k, v := range l.baseMetadata {
		childLogger.baseMetadata[k] = v
	}
	for k, v := range metadata {
		childLogger.baseMetadata[k] = v
	}

	childLogger.startFlushTimer()
	childLogger.setupShutdownHooks()

	return childLogger
}

func (l *Logger) CaptureError(err error, context map[string]interface{}) {
	if err == nil {
		return
	}

	stackTrace := make([]string, 0, 10)
	for i := 1; i < 10; i++ {
		_, file, line, ok := runtime.Caller(i)
		if !ok {
			break
		}
		stackTrace = append(stackTrace, fmt.Sprintf("%s:%d", file, line))
	}

	errorPayload := &ErrorPayload{
		Type:       fmt.Sprintf("%T", err),
		Message:    err.Error(),
		StackTrace: stackTrace,
		Context:    context,
	}

	entry := LogEntry{
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Level:       LevelError,
		Message:     err.Error(),
		Environment: l.environment,
		PrivacyMode: l.privacyMode,
		Error:       errorPayload,
		Metadata:    context,
	}

	if l.projectID != "" {
		entry.ProjectID = l.projectID
	}

	l.queueMu.Lock()
	l.queue = append(l.queue, entry)
	shouldFlush := len(l.queue) >= l.batchSize
	l.queueMu.Unlock()

	if shouldFlush {
		l.Flush()
	}
}

func (l *Logger) CaptureException(err error, context map[string]interface{}) {
	l.CaptureError(err, context)
}

func (l *Logger) Flush() {
	if l.shutdown {
		return
	}

	l.queueMu.Lock()
	if l.isFlushing || len(l.queue) == 0 {
		l.queueMu.Unlock()
		return
	}

	l.isFlushing = true
	logsToSend := make([]LogEntry, len(l.queue))
	copy(logsToSend, l.queue)
	l.queue = l.queue[:0]
	l.queueMu.Unlock()

	if err := l.sendBatch(logsToSend); err != nil {
		l.queueMu.Lock()
		l.queue = append(logsToSend, l.queue...)
		l.queueMu.Unlock()

		if l.debug {
			fmt.Printf("[LuminaLog] Failed to flush logs: %v\n", err)
		}
	} else if l.debug {
		fmt.Printf("[LuminaLog] Flushed %d logs\n", len(logsToSend))
	}

	l.queueMu.Lock()
	l.isFlushing = false
	l.queueMu.Unlock()
}

func (l *Logger) Shutdown() {
	if l.shutdown {
		return
	}

	l.shutdown = true
	l.stopFlushTimer()
	l.Flush()
}

func (l *Logger) log(level LogLevel, message string, metadata map[string]interface{}) {
	if l.minLevel != nil && !l.shouldLog(level) {
		return
	}

	entry := l.createEntry(level, message, metadata)

	l.queueMu.Lock()
	l.queue = append(l.queue, entry)
	shouldFlush := len(l.queue) >= l.batchSize
	l.queueMu.Unlock()

	if shouldFlush {
		l.Flush()
	}
}

func (l *Logger) shouldLog(level LogLevel) bool {
	if l.minLevel == nil {
		return true
	}

	minIndex := -1
	currentIndex := -1

	for i, lvl := range l.logLevels {
		if lvl == *l.minLevel {
			minIndex = i
		}
		if lvl == level {
			currentIndex = i
		}
	}

	return currentIndex >= minIndex
}

func (l *Logger) createEntry(level LogLevel, message string, metadata map[string]interface{}) LogEntry {
	finalMetadata := make(map[string]interface{})
	for k, v := range l.baseMetadata {
		finalMetadata[k] = v
	}
	for k, v := range metadata {
		finalMetadata[k] = v
	}

	entry := LogEntry{
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Level:       level,
		Message:     message,
		Environment: l.environment,
		PrivacyMode: l.privacyMode,
		Metadata:    finalMetadata,
	}

	if l.projectID != "" {
		entry.ProjectID = l.projectID
	}

	return entry
}

func (l *Logger) startFlushTimer() {
	l.flushTicker = time.NewTicker(l.flushInterval)
	go func() {
		for {
			select {
			case <-l.flushTicker.C:
				l.Flush()
			case <-l.stopChan:
				return
			}
		}
	}()
}

func (l *Logger) stopFlushTimer() {
	if l.flushTicker != nil {
		l.flushTicker.Stop()
	}
	close(l.stopChan)
}

func (l *Logger) setupShutdownHooks() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		l.Shutdown()
		os.Exit(0)
	}()
}

func (l *Logger) sendBatch(logs []LogEntry) error {
	batch := LogBatch{
		APIKey: l.apiKey,
		Logs:   logs,
	}

	body, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("failed to marshal logs: %w", err)
	}

	maxRetries := 3
	baseDelay := 1 * time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		req, err := http.NewRequest("POST", l.endpoint, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "luminalog-go/0.1.0")

		resp, err := l.httpClient.Do(req)
		if err != nil {
			isLastAttempt := attempt == maxRetries-1
			if isLastAttempt {
				return fmt.Errorf("failed to send request after %d attempts: %w", maxRetries, err)
			}

			delay := baseDelay * time.Duration(1<<uint(attempt))
			if l.debug {
				fmt.Printf("[LuminaLog] Attempt %d failed, retrying in %v...\n", attempt+1, delay)
			}
			time.Sleep(delay)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == 429 {
			fmt.Println("[LuminaLog] 🚨 LOG QUOTA EXCEEDED: Your plan limit has been reached. " +
				"Logs will be dropped until you upgrade. " +
				"Visit your dashboard to upgrade: https://luminalog.cloud/dashboard/billing")
			return nil
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			isLastAttempt := attempt == maxRetries-1
			if isLastAttempt {
				return fmt.Errorf("HTTP %d: request failed after %d attempts", resp.StatusCode, maxRetries)
			}

			delay := baseDelay * time.Duration(1<<uint(attempt))
			if l.debug {
				fmt.Printf("[LuminaLog] HTTP %d, retrying in %v...\n", resp.StatusCode, delay)
			}
			time.Sleep(delay)
			continue
		}

		var result IngestionResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil
		}

		if result.DebugUserID == "" && l.debug {
			fmt.Println("Warning: No debug_user_id in response")
		}

		return nil
	}

	return fmt.Errorf("failed to send logs after %d attempts", maxRetries)
}
