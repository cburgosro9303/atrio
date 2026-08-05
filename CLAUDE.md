# Atrio

Plataforma open source, de ejecución local, para desarrollo de software asistido por agentes de IA. Un ejecutable único en Go (CLI global `atrio` + portal web local con SPA React embebida) que orquesta CLIs de agentes instaladas en la máquina (primer proveedor: Claude Code) sobre monorepos autocontenidos donde el repositorio es la fuente de verdad del estado completo del proyecto.

## Mapa documental (leer bajo demanda, no cargar todo)

- `docs/spec/01-definicion-producto.md` — Visión, principios, reglas de negocio, roadmap M1→F4. Consultar para cualquier duda funcional o de alcance.
- `docs/spec/02-registro-adr.md` — ADRs 001–015 con contexto y alternativas. Consultar antes de cuestionar una decisión técnica.
- `docs/spec/03-arquitectura.md` — **Documento normativo de diseño**: topología, almacenes, paquetes, contrato del adaptador, esquemas, motor de flujos, modelo git, seguridad. Consultar SIEMPRE antes de decisiones de diseño o de crear un módulo nuevo.
- `docs/spec/04-backlog-m1.md` — Backlog ordenado por dependencias. Es la fuente de qué trabajar y en qué orden.

## Reglas innegociables

### Lenguaje y estructura
- Core en **Go**. Frontend del portal en React 19.x + TypeScript + Vite 8.x, embebido en el binario.
- Paquetes: `core/`, `store/`, `gitops/`, `providers/`, `flows/`, `api/`, `cli/`, `web/`.
- **Dependencias unidireccionales** (ADR-016): `core/` no importa ningún otro paquete del proyecto (dominio puro, sin I/O). Ningún paquete alcanza `cli/` ni `api/`, salvo dos excepciones nominales: `cmd/atrio → cli` (el main *es* la capa de entrega) y `cli → api` (el comando de portal levanta el servidor). `cmd/atrio` alcanza `api` solo transitivamente vía `cli`; importarla directa está prohibido. Violación = rechazo del cambio. **Excepción nueva = ADR nuevo**, nunca una edición al test.
- **Prohibido CGo**: rompe el cross-compile. SQLite se usa vía `modernc.org/sqlite` (driver Go puro).
- Código, identificadores y comentarios en **inglés**. Documentación de especificación en español (soporte bilingüe es→en llegará por i18n, no por traducción de código).

### Seguridad (prioridad #1 del proyecto)
- **Nunca shell interpolado**: toda invocación de procesos externos (git incluido) por array de argumentos.
- Git se delega al binario del sistema con salidas `--porcelain`; jamás bibliotecas git embebidas.
- El portal solo hace bind a localhost, exige token de sesión y ninguna acción destructiva por GET.

### Calidad
- `go test -race` obligatorio; los tests acompañan cada tarea, no se difieren.
- Todo artefacto JSON valida contra su JSON Schema; los esquemas llevan `schemaVersion`.
- IDs: ULID en todos los artefactos.

### Flujo de trabajo (dogfooding de las propias reglas de Atrio)
- Trabajar **una tarea del backlog por sesión**, respetando el orden de dependencias de `04-backlog-m1.md`.
- Una tarea = una rama: `task/{id-corto}-{slug}` (ej. `task/t001-bootstrap`).
- Al completar una tarea: actualizar su estado en `docs/spec/04-backlog-m1.md` en el mismo cambio.
- Commits atómicos con mensaje que referencia la tarea (ej. `T-001: bootstrap Go module and package skeleton`).
- **Push y comandos fuera del allowlist requieren aprobación del usuario** (configurado en `.claude/settings.json`).
- Para tareas de diseño (T-010, T-011, T-012, T-050): proponer plan y esperar aprobación antes de escribir código.

## Orquestación de modelos

- La **sesión principal corre en Opus** y actúa como orquestador: toma decisiones de diseño, planifica, y delega ejecución.
- Los **subagentes del proyecto** (`.claude/agents/`) corren en **claude-sonnet-5**: `explorer` (exploración de código/specs, solo lectura, protege el contexto principal), `implementer` (ejecución de tareas ya diseñadas, ideal para las marcadas ∥ en paralelo), `code-reviewer` (revisión contra las reglas duras antes de cada commit).
- Reparto: razonamiento arquitectónico y tareas de diseño (T-010, T-011, T-012, T-050) en la sesión principal; exploración, implementación mecánica y revisión delegadas a subagentes.
- Usa `code-reviewer` proactivamente antes de solicitar aprobación de cualquier commit.

## Comandos del proyecto

| Comando | Qué hace |
|---|---|
| `make verify` | Barrido previo a cualquier commit: `fmt-check` + `vet` + `lint` + `test` |
| `make build` | Compila el binario único en `bin/atrio` |
| `make build-all` | Cross-compile de las 5 plataformas; red temprana contra CGo accidental |
| `make test` | `go test -race ./...` |
| `make lint` | golangci-lint, pineado como tool dependency del módulo |
| `make fmt` | Aplica los formatters en sitio (`fmt-check` los verifica sin tocar nada) |
| `make tidy` | Reconcilia `go.mod`/`go.sum` |

**`CGO_ENABLED=0` va solo en los targets de build**, nunca global: fuera de darwin el toolchain rechaza `-race` sin cgo (`-race requires cgo`), así que forzarlo globalmente rompería los tests obligatorios en los runners de Linux y Windows. La garantía de cross-compile vive donde corresponde, en `build` y `build-all`.

**Caché de tests.** El test de arquitectura obtiene sus datos de un subproceso `go list`, invisible para la clave de caché de Go: una corrida cacheada reportaría verde sobre una violación real. Se ataca por dos vías — el test lee los fuentes auditados y hace `stat` de sus directorios para meterlos en la clave (cubre edición, alta de archivo y alta de paquete), y `make test` añade `-count=1`. Lo primero es lo que protege a quien invoque `go test` directo, que el allowlist del proyecto permite.

El test que hace cumplir las dependencias unidireccionales vive en `internal/archtest`. Se evalúa contra las 5 plataformas de la matriz — sincronizadas con `PLATFORMS` del Makefile por un test, no por un comentario — porque `go list` resuelve build constraints de una en una. Detecta violación directa, desde test interno y externo, lavado transitivo, alcance indebido de `cmd/atrio` a `api`, violación tras build tag y desaparición de un paquete. Huecos conocidos: `go list ./...` no expone paquetes bajo `testdata/`, ni archivos tras build tags personalizados (no hay ninguno todavía).

## Integración continua

`.github/workflows/ci.yml` (GitHub Actions) corre en `push` a `main`, `task/**` y `bug/**`, y en cada `pull_request`. Tres trabajos: `quality` (fmt-check, vet, lint y `go.mod`/`go.sum` limpios tras `tidy`, en un solo runner), `test` (`go test -race -count=1 ./...` en ubuntu, macos y windows, `fail-fast: false`) y `cross-compile` (`make build-all`).

`PLATFORMS` del Makefile es la **única** definición de la matriz: CI la consume vía `make build-all` y nunca reescribe la lista en YAML. El trabajo de tests invoca `go test` directo, no `make test`, porque el runner de Windows no trae GNU make. Esa duplicación es un riesgo real —debilitar el target local dejaría a CI corriendo la invocación vieja, verde sobre una puerta que ya no existe—, así que `internal/citest` la ata con un test: el comando del paso de CI debe ser idéntico al del target `test`, y el YAML no puede nombrar ninguna plataforma. Como con `archtest`: sincronizado por un test, no por un comentario.

Las acciones van ancladas por SHA de commit y `.github/dependabot.yml` las actualiza — acotado a `github-actions`, nunca a `gomod`.

## Esquemas de artefactos

`schemas/` es un paquete hoja fuera de los 8 de ADR-012: no importa nada del módulo y lo consumirán `store/`, `flows/` y `api/`. No es excepción a ADR-016 —ninguna dirección de dependencia cambia— pero queda anotado aquí porque el paquete existe. Los `.json` van embebidos con `go:embed`; `schemas/README.md` es el contrato legible y lleva la tabla de qué valida el esquema y qué valida el código.

Convenciones que un test hace cumplir: draft 2020-12, `$id` relativo idéntico al nombre del archivo, envolvente compuesta con `allOf` y cerrada con `unevaluatedProperties: false`, `title` y `description` no vacíos. Identificadores y enums **en inglés** (el bilingüismo llega por i18n). Una fixture inválida se llama `<propiedad>--<motivo>.json` y el test exige que el error del validador **apunte a esa propiedad** recorriendo el árbol de causas, no buscando subcadenas.

Hay **cuatro envolventes** y cada esquema lleva exactamente la suya, verificado por el test: la completa para los artefactos de proyecto, la reducida para el front-matter, `manifestEnvelope` para el manifiesto del marketplace y `definitionEnvelope` para las definiciones canónicas del catálogo —estas dos últimas no son artefactos de ningún proyecto; una definición además no lleva versión propia, porque su versión es el tag que la publica—. La familia por defecto es la de artefacto, así que un esquema nuevo queda sujeto a esa regla hasta que alguien decida otra cosa.

**Nunca `propertyNames`**, prohibido por un test a cualquier profundidad: el validador lo reporta con la ubicación de instancia vacía, así que el rechazo no puede nombrar el campo de la clave mala y se rompe la promesa del error reparador. Se usa `patternProperties` con `additionalProperties: false`. `common.schema.json` es el único sitio de cada concepto compartido; tres cosas están sincronizadas por test y no por comentario: las siete categorías de permisos, `catalogId` como prefijo literal de `catalogRef`, y los patrones que quedan duplicados por ser claves de `patternProperties`, que al ser expresiones regulares literales no admiten `$ref`.

## Estado actual

**Bloques 0 y 1 completos: T-001, T-002, T-010, T-011 y T-012.** Módulo `github.com/cburgosro9303/atrio`, Go 1.26, los 8 paquetes con sus `doc.go`, test de arquitectura, pipeline de CI y los 12 esquemas —los 9 de artefactos, el manifiesto del marketplace y las dos definiciones canónicas del catálogo— con su arnés de pruebas. **ADR-017** añadido (front-matter YAML validado, acota ADR-005).

Dos catálogos **no** están congelados en los esquemas, por la misma razón: `requiredCapabilities` y `blockingData[].key` llevan patrón y la pertenencia la verifica el core, para que la capacidad que aparezca en T-050 o la clave que traiga un flujo de F3 sean una release de plataforma y no un cambio incompatible de un contrato público. La dirección entre definiciones y catálogo es **la definición declara, el manifiesto indexa**: T-060 genera el manifiesto desde las definiciones, así que todo campo del manifiesto tiene que ser derivable de ellas.

Próximas tareas: el Bloque 1 ya no bloquea nada, así que abren en paralelo **T-020** (repositorio de artefactos JSON, depende de T-010), **T-030** (envoltura segura de git, depende de T-001) y **T-040** (entidades y máquinas de estado, depende de T-010). Riesgo prioritario a despejar temprano: spike de T-053 (arbitraje de permisos vía hooks de Claude Code) antes de congelar el diseño fino de T-050.
