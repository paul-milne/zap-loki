# zap-loki

This lib provides a sink for Zap that can send logs to a Loki instance via HTTP.

## Usage

```go
func initLogger() (*zap.Logger, error) {
    zapConfig := zap.NewProductionConfig()
    loki := zaploki.New(context.Background(), zaploki.Config{
        Url:          lokiAddress,
        BatchMaxSize: 1000,
        BatchMaxWait: 10 * time.Second,
        Labels:       map[string]string{"app": appName},
    })

    return loki.WithCreateLogger(zapConfig)
}
```