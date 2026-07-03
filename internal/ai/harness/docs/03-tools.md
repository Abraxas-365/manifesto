# 03 — Tools y Registry

Las herramientas son lo que el agente puede *hacer*. Se registran en un
`tool.Registry` y el agente las expone al modelo, ejecutándolas cuando el modelo
las pide.

Paquetes:
- `.../tool` — la interfaz `Tool` y el `Registry`
- `.../tool/builtins` — las herramientas por defecto (Read/Write/Edit/List/Glob/Grep/Bash)

## La interfaz `Tool`

```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() json.RawMessage
    Execute(ctx context.Context, input json.RawMessage) (*Result, error)
    IsReadOnly() bool
    RequiresApproval(input json.RawMessage) bool
}

type Result struct {
    Content string      // lo que ve el modelo
    IsError bool        // marca el resultado como error para el modelo
    Images  []ImageData // imágenes opcionales (base64)
}
```

- `Name` debe ser único en el registry.
- `Description` e `InputSchema` (JSON Schema) es lo que el modelo usa para decidir
  cuándo y cómo llamar la herramienta. Escríbelos con cuidado.
- `IsReadOnly` documenta si la herramienta muta estado.
- `RequiresApproval` decide si pasa por el `Approver` del agente (ver [01](./01-agent.md)).

## El Registry

```go
func NewRegistry() *Registry
func (r *Registry) Register(t Tool)          // sobrescribe si el nombre ya existe
func (r *Registry) Get(name string) (Tool, bool)
func (r *Registry) All() []Tool              // en orden de registro
func (r *Registry) Names() []string
```

El agente recibe el registry en `harness.New(provider, registry)`.

## Herramientas por defecto (builtins)

Tres constructores según tu backend. Todos devuelven un `*tool.Registry` listo.

```go
func Files(store fsys.Store) *tool.Registry
func Default(store fsys.Store, ex exec.Executor) *tool.Registry
func FromExecutor(ex exec.Executor) *tool.Registry
```

| Constructor | Registra | Cuándo usarlo |
|-------------|----------|---------------|
| `Files(store)` | Read, Write, Edit, List, Glob, Grep | Solo almacenamiento, sin shell (p.ej. S3). |
| `Default(store, ex)` | Lo de `Files` + Bash (+ BashOutput/KillShell si el executor soporta background) | Cuando emparejas a propósito un store y un executor. |
| `FromExecutor(ex)` | Igual que `Default` pero el store se **deriva** del mismo executor | Docker, SSH, cualquier executor remoto: evita el "split-brain". |

> **Split-brain**: si las herramientas de fichero y Bash apuntan a entornos
> distintos, el agente lee un mundo y ejecuta en otro. `FromExecutor` lo hace
> imposible por construcción. Ver [04 — Backends](./04-backends.md).

Ejemplo local:

```go
fs, _ := fsxlocal.NewLocalFileSystem(".")
ex := exec.NewLocalExecutor(".")
registry := builtins.Default(fsxstore.New(fs), ex)
```

Las seis herramientas de fichero operan sobre `fsys.Store`; Bash sobre
`exec.Executor`. Ambos backends se explican en [04](./04-backends.md).

## Escribir una herramienta propia

Implementa los seis métodos. Ejemplo: una herramienta que devuelve la hora.

```go
type NowTool struct{}

func (NowTool) Name() string        { return "now" }
func (NowTool) Description() string  { return "Devuelve la hora actual en RFC3339." }
func (NowTool) InputSchema() json.RawMessage {
    return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (NowTool) IsReadOnly() bool                              { return true }
func (NowTool) RequiresApproval(json.RawMessage) bool          { return false }

func (NowTool) Execute(_ context.Context, _ json.RawMessage) (*tool.Result, error) {
    return &tool.Result{Content: time.Now().Format(time.RFC3339)}, nil
}
```

Registrarla:

```go
registry.Register(NowTool{})
```

Si el input tiene campos, deserialízalo dentro de `Execute`:

```go
func (t GreetTool) Execute(_ context.Context, input json.RawMessage) (*tool.Result, error) {
    var in struct{ Name string `json:"name"` }
    if err := json.Unmarshal(input, &in); err != nil {
        return &tool.Result{Content: "invalid input: " + err.Error(), IsError: true}, nil
    }
    return &tool.Result{Content: "Hola, " + in.Name}, nil
}
```

> Devuelve errores de *uso* (input inválido, fichero no encontrado) como
> `Result{IsError: true}` para que el modelo pueda corregirse. Reserva el
> `error` de retorno para fallos reales de infraestructura.

## Herramientas diferidas (deferred)

Cuando registras muchas herramientas, puedes ocultar su esquema al modelo hasta
que las busque, ahorrando contexto. Ver [08 — ToolSearch](./08-toolsearch.md).

Ver el ejemplo runnable [`examples/08_custom_tool`](../examples/08_custom_tool).
