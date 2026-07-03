# 02 — Providers

Un `Provider` es el backend LLM. El agente solo habla con esta interfaz, así que
puedes cambiar de OpenAI a Anthropic (o a un mock en tests) sin tocar el resto.

Paquete: `github.com/Abraxas-365/manifesto/internal/ai/harness/llm`

## La interfaz

```go
type Provider interface {
    Chat(ctx context.Context, req Request) (*Response, error)
    ChatStream(ctx context.Context, req Request) (Stream, error)
}
```

El agente construye el `Request` por ti a partir de sus campos (`System`, `Model`,
`MaxTokens`, `Temperature`, `TopP`, `Reasoning`, `ProviderOptions`) más el
historial y las herramientas. No necesitas construir `Request` a mano salvo que
llames al provider directamente.

## OpenAI

Paquete: `.../llm/openai`

```go
func New(apiKey string, opts ...option.RequestOption) *Provider
```

Si `apiKey` es `""`, usa la variable de entorno `OPENAI_API_KEY`.

```go
import "github.com/Abraxas-365/manifesto/internal/ai/harness/llm/openai"

provider := openai.New(os.Getenv("OPENAI_API_KEY"))
agent := harness.New(provider, registry)
agent.Model = "gpt-4o"   // DefaultModel = "gpt-4o" si lo dejas vacío
```

Constantes: `openai.DefaultModel = "gpt-4o"`, `openai.DefaultMaxTokens = 4096`.

Los `option.RequestOption` son del SDK oficial de OpenAI, útiles para apuntar a
un endpoint compatible (Azure, OpenRouter, un proxy local):

```go
import "github.com/openai/openai-go/v3/option"

provider := openai.New("", option.WithBaseURL("http://localhost:11434/v1"))
```

## Anthropic

Paquete: `.../llm/anthropic`

```go
func New(apiKey string, opts ...option.RequestOption) *Provider
func NewWithOptions(apiKey string, cacheOpts []Option, sdkOpts ...option.RequestOption) *Provider
```

`New` usa `ANTHROPIC_API_KEY` cuando `apiKey` es `""`. `NewWithOptions` permite
además configurar el caché de prompts vía las `Option` del paquete.

```go
import "github.com/Abraxas-365/manifesto/internal/ai/harness/llm/anthropic"

provider := anthropic.New(os.Getenv("ANTHROPIC_API_KEY"))
agent := harness.New(provider, registry)
agent.Model = "claude-sonnet-4-20250514"
```

## Capacidades por modelo

Distintos modelos aceptan distintos parámetros (algunos no permiten
`temperature`, otros sí razonan). El paquete `llm` mantiene una tabla y expone:

```go
func Capabilities(model string) Capability
```

Los adapters la consultan automáticamente: si pones `Temperature` en un modelo que
no la soporta, se **omite** en vez de fallar. No tienes que hacer nada, pero
puedes inspeccionarla:

```go
if llm.Capabilities(agent.Model).SupportsTemperature {
    t := 0.2
    agent.Temperature = &t
}
```

Ver [12 — Provider options](./12-provider-options.md) para el detalle de
`Temperature`, `TopP`, `Reasoning` y el bag `ProviderOptions`.

## Combinar varios providers

Para elegir el provider según el patrón del modelo (p.ej. `gpt-*` → OpenAI,
`claude-*` → Anthropic) usa el **router**: ver [05 — Retry y Router](./05-retry-router.md).

## Implementar tu propio provider

Solo tienes que satisfacer los dos métodos. Un mock mínimo para tests:

```go
type mockProvider struct{}

func (mockProvider) Chat(_ context.Context, _ llm.Request) (*llm.Response, error) {
    return &llm.Response{
        Message: llm.Message{
            Role:    llm.RoleAssistant,
            Content: []llm.ContentBlock{llm.Text("hola")},
        },
        StopReason: llm.StopEndTurn,
    }, nil
}

func (mockProvider) ChatStream(context.Context, llm.Request) (llm.Stream, error) {
    return nil, errors.New("streaming no soportado")
}
```

Si no vas a usar streaming, `ChatStream` puede devolver un error: el agente usa
`Chat`.
