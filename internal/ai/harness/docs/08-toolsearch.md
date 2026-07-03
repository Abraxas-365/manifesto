# 08 — ToolSearch (herramientas diferidas)

Cuando registras muchas herramientas, mandar todos sus esquemas al modelo en cada
turno gasta contexto y lo distrae. Las herramientas **diferidas** se anuncian solo
por nombre + un hint corto; el modelo carga el esquema completo bajo demanda con
la herramienta `ToolSearch`.

Paquetes:
- `github.com/Abraxas-365/manifesto/internal/ai/harness` — `Agent.EnableToolSearch`
- `.../tool` — `Registry.SetDeferred`, `tool.Deferrable`, `Discovery`
- `.../toolsearch` — la meta-herramienta `ToolSearch`

## Activarlo

```go
func (a *Agent) EnableToolSearch() *Agent
```

`EnableToolSearch` crea una `Discovery` compartida y registra la herramienta
`ToolSearch` ligada al registry del agente. A partir de ahí, las herramientas
marcadas como diferidas se envían al modelo como nombre+hint (en un
`system-reminder`) hasta que las revele vía `ToolSearch`.

```go
agent := harness.New(provider, registry)
registry.SetDeferred("Grep", "buscar texto con regex en ficheros")
registry.SetDeferred("Bash", "ejecutar comandos de shell")
agent.EnableToolSearch()
```

> `EnableToolSearch` hace **panic** si `SetDeferred` apunta a un nombre no
> registrado (típicamente un typo). Falla en el wiring, no en producción.

## Dos formas de diferir una herramienta

**1. Por registry** (para builtins u herramientas ajenas):

```go
func (r *Registry) SetDeferred(name, hint string)
```

**2. Embebiendo `tool.Deferrable`** (para tus propias herramientas):

```go
type MyTool struct {
    tool.Deferrable // aporta ShouldDefer()=true y SearchHint()
    // ...
}

func NewMyTool() *MyTool {
    return &MyTool{Deferrable: tool.Deferrable{Hint: "hace X con Y"}}
}
```

Un override con `SetDeferred` gana sobre el `Deferrable` embebido.

## Cómo lo usa el modelo

La herramienta `ToolSearch` (constante `toolsearch.ToolName = "ToolSearch"`)
acepta un `query`:

- `select:Nombre` o `select:Read,Edit,Grep` — revela esas herramientas exactas.
- palabras clave — busca por nombre y hint, revela las mejores coincidencias.

Una vez revelada, su esquema completo entra en los turnos siguientes y el modelo
puede llamarla normalmente. El estado de revelado vive en la `Discovery`
compartida durante toda la sesión (`Run` sucesivos).

## Validar el wiring

```go
if unknown := registry.DeferredUnknown(); len(unknown) > 0 {
    log.Fatalf("deferred desconocidos: %v", unknown)
}
```

`EnableToolSearch` ya lo hace por ti (con panic), pero puedes comprobarlo antes si
prefieres un error controlado.

## Cuándo usarlo

- Tienes decenas de herramientas y el prompt se infla.
- Herramientas raras o peligrosas que no quieres tentar al modelo a usar por
  defecto.
- No lo necesitas para un puñado de herramientas: el coste supera el beneficio.

Ver el ejemplo runnable [`examples/04_toolsearch`](../examples/04_toolsearch).
