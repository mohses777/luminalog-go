<div align="center">
  <h1>luminalog-go</h1>
  <p>Privacy-first logging with AI-powered debugging for Go</p>
  
  [![Go Reference](https://pkg.go.dev/badge/github.com/luminalog/sdk-go.svg)](https://pkg.go.dev/github.com/luminalog/sdk-go)
  [![Go Report Card](https://goreportcard.com/badge/github.com/luminalog/sdk-go)](https://goreportcard.com/report/github.com/luminalog/sdk-go)
  [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
  [![Go Version](https://img.shields.io/github/go-mod/go-version/luminalog/sdk-go)](https://golang.org)
  
  <p>
    <a href="#installation">Installation</a> •
    <a href="#quick-start">Quick Start</a> •
    <a href="#documentation">Documentation</a> •
    <a href="#examples">Examples</a> •
    <a href="#support">Support</a>
  </p>
</div>

---

## Features

- 🔒 **Privacy-First** - Automatic PII scrubbing on the server
- ⚡ **Zero Performance Impact** - Async batching (100 logs or 5s intervals)
- 🛡️ **Graceful Degradation** - Queues logs locally if API is unavailable
- 📦 **Type-Safe** - Full type safety with Go structs
- 🪶 **Zero Dependencies** - Only uses Go standard library
- 🎯 **Error Tracking** - Automatic error grouping and stack traces
- 🔄 **Goroutine-Safe** - Thread-safe with mutex locks
- 📊 **Quota Management** - Built-in quota exceeded handling

## Installation

```bash
go get github.com/luminalog/sdk-go
```

## Quick Start

```go
package main

import (
    "log"
    "github.com/luminalog/sdk-go/luminalog"
)

func main() {
    logger, err := luminalog.New(luminalog.Config{
        APIKey:      "your-api-key",
        Environment: "production",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer logger.Shutdown()

    // Basic logging
    logger.Info("User logged in", map[string]interface{}{
        "user_id": "123",
    })

    logger.Warn("High memory usage", map[string]interface{}{
        "memory_mb": 512,
    })

    logger.Error("Payment failed", map[string]interface{}{
        "error": "Card declined",
    })

    // Critical errors (sent immediately, bypasses batching)
    logger.Panic("Database connection lost!", nil)
}
```

## Configuration

### Options

```go
type Config struct {
    // Required
    APIKey      string        // Your LuminaLog API key

    // Optional
    Environment   string        // Label for filtering logs (stored as metadata). Actual environment is set by your API key.
    ProjectID     string        // Project identifier
    BatchSize     int           // Logs before auto-flush (default: 100)
    FlushInterval time.Duration // Time between flushes (default: 5s)
    Endpoint      string        // Custom API endpoint
    Debug         bool          // Enable debug logging (default: false)
}
```

> **Note:** The `Environment` parameter is stored as metadata for filtering within your logs. The actual environment (production, staging, etc.) is determined by your API key's environment setting in the dashboard. This prevents quota abuse and ensures clear project boundaries.

### Environment Variables

Store your API key securely using environment variables:

```bash
# .env
LUMINALOG_API_KEY=your-api-key-here
```

```go
import (
    "os"
    "github.com/luminalog/sdk-go/luminalog"
)

logger, err := luminalog.New(luminalog.Config{
    APIKey:      os.Getenv("LUMINALOG_API_KEY"),
    Environment: os.Getenv("ENVIRONMENT"),
})
```

## API Reference

### Log Levels

| Level   | Method           | Description                    | Behavior        |
| ------- | ---------------- | ------------------------------ | --------------- |
| `Debug` | `logger.Debug()` | Detailed debugging information | Batched         |
| `Info`  | `logger.Info()`  | General operational messages   | Batched         |
| `Warn`  | `logger.Warn()`  | Warning conditions             | Batched         |
| `Error` | `logger.Error()` | Error conditions               | Batched         |
| `Fatal` | `logger.Fatal()` | Fatal errors                   | Batched         |
| `Panic` | `logger.Panic()` | Critical errors                | Immediate flush |

### Methods

#### `logger.Debug(message, metadata)`

Log a debug message.

```go
logger.Debug("Cache hit", map[string]interface{}{
    "key": "user:123",
    "ttl": 3600,
})
```

#### `logger.Info(message, metadata)`

Log an informational message.

```go
logger.Info("User logged in", map[string]interface{}{
    "user_id": "123",
    "ip":      "192.168.1.1",
})
```

#### `logger.Warn(message, metadata)`

Log a warning message.

```go
logger.Warn("API rate limit approaching", map[string]interface{}{
    "current": 950,
    "limit":   1000,
})
```

#### `logger.Error(message, metadata)`

Log an error message.

```go
logger.Error("Payment processing failed", map[string]interface{}{
    "user_id": "123",
    "amount":  99.99,
    "error":   "Card declined",
})
```

#### `logger.Fatal(message, metadata)`

Log a fatal error.

```go
logger.Fatal("Critical service unavailable", map[string]interface{}{
    "service": "database",
})
```

#### `logger.Panic(message, metadata)`

Log a critical error and flush immediately.

```go
logger.Panic("Database connection lost!", map[string]interface{}{
    "host": "db.example.com",
})
```

#### `logger.CaptureError(error, context)`

Capture an error with full stack trace.

```go
if err := riskyOperation(); err != nil {
    logger.CaptureError(err, map[string]interface{}{
        "user_id":   "123",
        "operation": "payment_processing",
    })
}
```

#### `logger.Flush()`

Manually flush all queued logs.

```go
logger.Flush()
```

#### `logger.Shutdown()`

Stop the logger and flush remaining logs.

```go
logger.Shutdown()
```

## Examples

### Gin Framework

```go
package main

import (
    "os"
    "github.com/gin-gonic/gin"
    "github.com/luminalog/sdk-go/luminalog"
)

func main() {
    logger, err := luminalog.New(luminalog.Config{
        APIKey: os.Getenv("LUMINALOG_API_KEY"),
    })
    if err != nil {
        panic(err)
    }
    defer logger.Shutdown()

    r := gin.Default()

    // Logging middleware
    r.Use(func(c *gin.Context) {
        logger.Info("Request", map[string]interface{}{
            "method": c.Request.Method,
            "path":   c.Request.URL.Path,
            "ip":     c.ClientIP(),
        })
        c.Next()
    })

    // Error handling middleware
    r.Use(func(c *gin.Context) {
        c.Next()
        if len(c.Errors) > 0 {
            for _, err := range c.Errors {
                logger.CaptureError(err, map[string]interface{}{
                    "path": c.Request.URL.Path,
                })
            }
        }
    })

    r.GET("/", func(c *gin.Context) {
        c.JSON(200, gin.H{"message": "Hello World"})
    })

    r.Run(":8080")
}
```

### Echo Framework

```go
package main

import (
    "os"
    "github.com/labstack/echo/v4"
    "github.com/luminalog/sdk-go/luminalog"
)

func main() {
    logger, err := luminalog.New(luminalog.Config{
        APIKey: os.Getenv("LUMINALOG_API_KEY"),
    })
    if err != nil {
        panic(err)
    }
    defer logger.Shutdown()

    e := echo.New()

    // Logging middleware
    e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            logger.Info("Request", map[string]interface{}{
                "method": c.Request().Method,
                "path":   c.Request().URL.Path,
                "ip":     c.RealIP(),
            })
            return next(c)
        }
    })

    // Error handling middleware
    e.HTTPErrorHandler = func(err error, c echo.Context) {
        logger.CaptureError(err, map[string]interface{}{
            "path": c.Request().URL.Path,
        })
        e.DefaultHTTPErrorHandler(err, c)
    }

    e.GET("/", func(c echo.Context) error {
        return c.String(200, "Hello World")
    })

    e.Start(":8080")
}
```

### Standard HTTP Server

```go
package main

import (
    "net/http"
    "os"
    "github.com/luminalog/sdk-go/luminalog"
)

func main() {
    logger, err := luminalog.New(luminalog.Config{
        APIKey: os.Getenv("LUMINALOG_API_KEY"),
    })
    if err != nil {
        panic(err)
    }
    defer logger.Shutdown()

    // Logging middleware
    loggingMiddleware := func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            logger.Info("Request", map[string]interface{}{
                "method": r.Method,
                "path":   r.URL.Path,
                "ip":     r.RemoteAddr,
            })
            next.ServeHTTP(w, r)
        })
    }

    mux := http.NewServeMux()
    mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("Hello World"))
    })

    http.ListenAndServe(":8080", loggingMiddleware(mux))
}
```

### gRPC Server

```go
package main

import (
    "context"
    "os"
    "google.golang.org/grpc"
    "github.com/luminalog/sdk-go/luminalog"
)

func main() {
    logger, err := luminalog.New(luminalog.Config{
        APIKey: os.Getenv("LUMINALOG_API_KEY"),
    })
    if err != nil {
        panic(err)
    }
    defer logger.Shutdown()

    // Unary interceptor
    unaryInterceptor := func(
        ctx context.Context,
        req interface{},
        info *grpc.UnaryServerInfo,
        handler grpc.UnaryHandler,
    ) (interface{}, error) {
        logger.Info("gRPC call", map[string]interface{}{
            "method": info.FullMethod,
        })

        resp, err := handler(ctx, req)
        if err != nil {
            logger.CaptureError(err, map[string]interface{}{
                "method": info.FullMethod,
            })
        }
        return resp, err
    }

    s := grpc.NewServer(
        grpc.UnaryInterceptor(unaryInterceptor),
    )

    // Register your services here
    // pb.RegisterYourServiceServer(s, &yourService{})
}
```

### AWS Lambda

```go
package main

import (
    "context"
    "os"
    "github.com/aws/aws-lambda-go/lambda"
    "github.com/luminalog/sdk-go/luminalog"
)

var logger *luminalog.Logger

func init() {
    var err error
    logger, err = luminalog.New(luminalog.Config{
        APIKey:      os.Getenv("LUMINALOG_API_KEY"),
        Environment: "production",
    })
    if err != nil {
        panic(err)
    }
}

func HandleRequest(ctx context.Context, event map[string]interface{}) (string, error) {
    defer logger.Shutdown()

    logger.Info("Lambda invoked", map[string]interface{}{
        "event_type": event["type"],
    })

    // Your logic here
    result, err := processEvent(event)
    if err != nil {
        logger.CaptureError(err, map[string]interface{}{
            "event": event,
        })
        return "", err
    }

    return result, nil
}

func main() {
    lambda.Start(HandleRequest)
}
```

## Documentation

- 📘 [Full Documentation](https://luminalog.cloud/docs)
- 🚀 [Getting Started Guide](https://luminalog.cloud/docs/getting-started)
- 📖 [API Reference](https://luminalog.cloud/docs/api)
- 💡 [Best Practices](https://luminalog.cloud/docs/best-practices)

## Support

- 🐛 [Report a Bug](https://github.com/luminalog/sdk-go/issues)
- 💬 [Discord Community](https://discord.gg/luminalog)
- 📧 [Email Support](mailto:support@luminalog.cloud)
- 📚 [Knowledge Base](https://luminalog.cloud/docs)

## Contributing

We welcome contributions! Please see our [Contributing Guide](../../CONTRIBUTING.md) for details.

## License

MIT © [LuminaLog Team](https://luminalog.cloud)

---

<div align="center">
  <p>Built with ❤️ by the LuminaLog team</p>
  <p>
    <a href="https://luminalog.cloud">Website</a> •
    <a href="https://luminalog.cloud/docs">Docs</a> •
    <a href="https://twitter.com/luminalog">Twitter</a> •
    <a href="https://github.com/luminalog/sdk-go">GitHub</a>
  </p>
</div>
