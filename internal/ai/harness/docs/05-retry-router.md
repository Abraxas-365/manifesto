# 05 — Retry y Router

Dos providers-decoradores. Ambos satisfacen `llm.Provider`, así que envuelven a
otro provider de forma transparente y se pueden anidar.

Paquetes:
- `.../llm/retry` — reintentos con backoff exponencial
- `.../llm/router` — despacha por patrón de modelo

## Retry

Envuelve un provider para reintentar fallos transitorios (errores de red, 429,
5xx) con backoff exponencial y jitter. Los errores de contexto (cancelado,
deadline) no se reintentan.

```go
func Wrap(next llm.Provider, opts ...Option) *Provider
```

Opciones:

```go
func WithMaxAttempts(n int) Option        // default 4
func WithBaseDelay(d time.Duration) Option // default 500ms
func WithMaxDelay(d time.Duration) Option  // default 30s
func WithOnRetry(fn func(attempt int, err error, delay time.Duration)) Option
```

Uso directo:

```go
import "github.com/Abraxas-365/manifesto/internal/ai/harness/llm/retry"

provider := retry.Wrap(openai.New(""), retry.WithMaxAttempts(5))
agent := harness.New(provider, registry)
```

### Vía el agente (recomendado)

`Agent.EnableRetry` envuelve el provider del agente y puentea los reintentos a
`Hooks.OnRetry`, así los ves por el mismo canal que el resto del bucle:

```go
agent := harness.New(openai.New(""), registry).EnableRetry()

// o con opciones:
agent.EnableRetry(retry.WithMaxAttempts(6), retry.WithBaseDelay(time.Second))
```

`OnRetry` se lee en tiempo de ejecución, así que puedes fijar el hook antes o
después de `EnableRetry`. Ver [06 — Hooks](./06-hooks.md).

## Router

Elige el provider según el patrón del nombre del modelo. Útil cuando un agente
(o un subagente) puede usar modelos de varios proveedores.

```go
func New(opts ...Option) *Router
func WithDefault(p llm.Provider) Option

func (r *Router) Handle(model string, p llm.Provider) *Router          // nombre exacto
func (r *Router) HandlePattern(pattern string, p llm.Provider) *Router // glob (filepath.Match)
```

`Handle`/`HandlePattern` devuelven el propio router para encadenar. Los patrones
se evalúan en orden inverso de registro: **el último que hace match gana**. Si
ninguno coincide se usa el default; si no hay default, `Chat` devuelve `ErrNoRoute`.

```go
import "github.com/Abraxas-365/manifesto/internal/ai/harness/llm/router"

r := router.New(router.WithDefault(openai.New(""))).
    HandlePattern("gpt-*", openai.New("")).
    HandlePattern("claude-*", anthropic.New(""))

agent := harness.New(r, registry)
agent.Model = "claude-sonnet-4-20250514" // → Anthropic
```

## Combinarlos

Como ambos son `llm.Provider`, se anidan. Normalmente el retry envuelve al router
para reintentar sea cual sea el backend elegido:

```go
r := router.New(router.WithDefault(openai.New(""))).
    HandlePattern("gpt-*", openai.New("")).
    HandlePattern("claude-*", anthropic.New(""))

agent := harness.New(r, registry).EnableRetry()
```

El router también es la pieza que permite a un subagente elegir modelo: ver
[10 — Subagents](./10-subagents.md).

Ver el ejemplo runnable [`examples/02_router`](../examples/02_router) y
[`examples/03_retry_hooks`](../examples/03_retry_hooks).
