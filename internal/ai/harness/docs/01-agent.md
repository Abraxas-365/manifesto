# 01 — Agent

El `Agent` es el bucle de llamada a herramientas: recibe texto del usuario, habla
con el modelo, ejecuta las herramientas que el modelo pide, y repite hasta que el
modelo da una respuesta final.

Paquete: `github.com/Abraxas-365/manifesto/internal/ai/harness`

## Crear un agente

```go
func New(provider llm.Provider, registry *tool.Registry) *Agent
```

`New` solo asigna `Provider` y `Registry`. Todo lo demás se configura poniendo
campos en el struct devuelto:

```go
agent := harness.New(provider, registry)
agent.System = "You are a helpful coding assistant."
agent.Model = "gpt-4o"
```

## Ejecutar

```go
func (a *Agent) Run(ctx context.Context, userInput string) (string, error)
```

`Run` conduce el bucle completo para un input del usuario y devuelve el **texto
final** del asistente. El estado de la conversación **persiste entre llamadas**:
llama `Run` varias veces sobre el mismo `Agent` y mantiene el historial.

```go
answer, err := agent.Run(ctx, "Lista los ficheros Go")
answer, err = agent.Run(ctx, "Ahora resume el primero")  // recuerda el turno anterior
```

### Cómo funciona el bucle (alto nivel)

Por cada turno (hasta `MaxTurns`):
1. Dispara `Hooks.OnTurnStart`.
2. Compacta el historial si hace falta (ver [07](./07-compaction.md)).
3. Llama a `Provider.Chat` con el historial, el system prompt y las herramientas.
4. Suma el `Usage` y dispara `Hooks.OnUsage`.
5. Si el modelo **no** pidió herramientas → devuelve su texto (fin).
6. Si pidió herramientas → ejecuta cada una, añade los resultados al historial, y
   vuelve al paso 1.

Si se agota `MaxTurns` sin respuesta final, devuelve `ErrMaxTurns`.

## Campos del struct `Agent`

### Básicos

| Campo | Tipo | Por defecto | Descripción |
|-------|------|-------------|-------------|
| `Provider` | `llm.Provider` | — | El proveedor LLM (ver [02](./02-providers.md)). |
| `Registry` | `*tool.Registry` | — | Las herramientas disponibles (ver [03](./03-tools.md)). |
| `System` | `string` | `""` | El system prompt. |
| `Model` | `string` | depende del provider | ID del modelo, p.ej. `"gpt-4o"`. |
| `MaxTokens` | `int` | lo pone el provider | Máx. tokens de salida por turno. |
| `MaxTurns` | `int` | `25` (`DefaultMaxTurns`) | Máx. iteraciones del bucle por `Run`. |

### Opciones de request (ver [12](./12-provider-options.md))

| Campo | Tipo | Descripción |
|-------|------|-------------|
| `Temperature` | `*float64` | Nil = no se envía. Se omite en modelos que no la soportan. |
| `TopP` | `*float64` | Nil = no se envía. |
| `Reasoning` | `llm.ReasoningLevel` | Esfuerzo de razonamiento agnóstico al provider. Vacío = ninguno. |
| `ProviderOptions` | `map[string]map[string]any` | Opciones crudas por provider (`"openai"`, `"anthropic"`). |

### Aprobación de herramientas

| Campo | Tipo | Descripción |
|-------|------|-------------|
| `Approver` | `Approver` | Gatea las herramientas cuyo `RequiresApproval` es true. Nil = auto-aprobar (headless). |

```go
type Approver func(t tool.Tool, input json.RawMessage) bool
```

Si una herramienta requiere aprobación y el `Approver` devuelve `false`, la
llamada se rechaza y el modelo recibe "Tool execution denied by approver".

```go
agent.Approver = func(t tool.Tool, input json.RawMessage) bool {
    fmt.Printf("¿Permitir %s con %s? [y/N] ", t.Name(), input)
    var r string
    fmt.Scanln(&r)
    return r == "y"
}
```

### Observabilidad y contexto

| Campo | Tipo | Descripción |
|-------|------|-------------|
| `Hooks` | `Hooks` | Callbacks opcionales. Zero value = no-op. Ver [06](./06-hooks.md). |
| `Compactor` | `Compactor` | Compacta el historial. Nil = desactivado. Ver [07](./07-compaction.md). |
| `CompactThreshold` | `float64` | Fracción de la ventana a la que compacta. 0 = `0.8`. |
| `ContextWindow` | `int` | Sobrescribe la ventana del modelo. 0 = se busca por `Model`. |
| `TokenEstimator` | `TokenEstimator` | Sobrescribe el estimador de tokens por defecto. |

## Métodos

```go
func (a *Agent) Run(ctx context.Context, userInput string) (string, error)
func (a *Agent) History() []llm.Message   // la conversación acumulada
func (a *Agent) TotalUsage() llm.Usage    // tokens acumulados en todas las llamadas a Run
func (a *Agent) EnableRetry(opts ...retry.Option) *Agent      // ver 05
func (a *Agent) EnableToolSearch() *Agent                     // ver 08
```

`EnableRetry` y `EnableToolSearch` devuelven el propio agente para encadenar:

```go
agent := harness.New(provider, registry).EnableRetry()
```

## Errores

Producidos con el paquete `errx` (registro `HARNESS_AGENT`):

| Error | Cuándo |
|-------|--------|
| `ErrMaxTurns` | Se agotó `MaxTurns` sin respuesta final. Detalle: `max_turns`. |
| `ErrMaxTokens` | El modelo cortó por límite de tokens sin terminar (y sin pedir herramienta). El texto parcial va en el detalle `partial`. |

## Ejemplo completo

```go
fs, _ := fsxlocal.NewLocalFileSystem(".")
ex := exec.NewLocalExecutor(".")
registry := builtins.Default(fsxstore.New(fs), ex)

agent := harness.New(openai.New(os.Getenv("OPENAI_API_KEY")), registry)
agent.System = "You are a focused coding assistant."
agent.Model = "gpt-4o"
agent.MaxTurns = 15

answer, err := agent.Run(context.Background(), "¿Cuántos ficheros .go hay aquí?")
if err != nil {
    log.Fatal(err)
}
fmt.Println(answer)
fmt.Printf("tokens usados: %+v\n", agent.TotalUsage())
```

Ver el ejemplo runnable [`examples/01_minimal`](../examples/01_minimal).
