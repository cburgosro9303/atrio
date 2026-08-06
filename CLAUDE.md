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

**`format` se afirma por consumidor, no por el contrato.** `timestamp` y `actor.email` se apoyan solo en `format`, sin `pattern` de respaldo, y la aserción va apagada por defecto en la librería: el arnés de `schemas/` **no comprueba** que una fecha sea una fecha ni que un email lo sea. Hoy el único consumidor que la activa es `store/`, verificado antes contra todas las fixtures válidas para confirmar que activarla no estrecha lo ya congelado. Quien compile estos esquemas por su cuenta (`flows/`, `api/`) hereda el hueco si no la activa.

## Estado actual

**Completadas: T-001, T-002, T-010, T-011, T-012, T-020, T-021, T-030 y T-040.** Módulo `github.com/cburgosro9303/atrio`, Go 1.26, los 8 paquetes con sus `doc.go`, test de arquitectura, pipeline de CI, los 12 esquemas —los 9 de artefactos, el manifiesto del marketplace y las dos definiciones canónicas del catálogo—, el repositorio de artefactos JSON y la base local SQLite en `store/`, la envoltura segura de git en `gitops/` y el dominio puro en `core/`. **ADR-017** añadido (front-matter YAML validado, acota ADR-005).

Dos catálogos **no** están congelados en los esquemas, por la misma razón: `requiredCapabilities` y `blockingData[].key` llevan patrón y la pertenencia la verifica el core, para que la capacidad que aparezca en T-050 o la clave que traiga un flujo de F3 sean una release de plataforma y no un cambio incompatible de un contrato público. La dirección entre definiciones y catálogo es **la definición declara, el manifiesto indexa**: T-060 genera el manifiesto desde las definiciones, así que todo campo del manifiesto tiene que ser derivable de ellas.

**Los tres paquetes de la última tanda no se importan entre sí todavía.** `store/` toma la identidad git por inyección en vez de importar `gitops/`, y no invoca los predicados de `core/`. La frontera que los separa es una sola frase: *una regla que se decide con valores que ya están en memoria es de `core/`; una que exige tocar el filesystem, git u otro artefacto almacenado es de `store/`*, con dos excepciones que la tabla de `schemas/README.md` concede a `store/` porque se deciden sin salir del documento (inmutabilidad de `artifactLanguage`, unicidad de `displayName`). El cableado es de la tarea que construya el mapeo JSON↔dominio, que además arrastra un residual conocido: **nada ata mecánicamente los enums de `core/` a los de `schemas/`**, porque `core/` no puede importar `schemas/` sin violar ADR-016; hoy coinciden uno a uno, pero una edición futura de un esquema derivaría en silencio.

`make build-all` **no alcanza `store/` ni `core/`**: `cmd/atrio` todavía no los importa, así que la red de cross-compile de CI no los cubre. Hasta que algún comando los alcance, esa comprobación hay que hacerla a mano — y con `modernc.org/sqlite` dentro, esa comprobación es justamente la que confirma que la prohibición de CGo sigue en pie.

## Base local SQLite (`store/localdb.go` + `store/localdb.sql`)

Vive en `.atrio/atrio.db`, gitignorada, con los sidecars `-wal`/`-shm` que crea el modo WAL —los tres patrones tienen que estar en el `.gitignore`, incluido el que genere `init`: sin ellos `gitops.Status` reporta el worktree sucio por archivos de la plataforma—. Driver `modernc.org/sqlite` (ADR-007), sin CGo.

**No hay migración: hay generación.** El `pragma user_version` guarda la generación del DDL; una que este build no escribió hace que el archivo se descarte y se reconstruya. Lo autoriza ADR-006 —todo aquí es derivable *o efímero*— y la clasificación tabla por tabla la hace cumplir un test, no un comentario: una **tabla real** nueva sin clase declarada rompe la suite (la virtual `document_fts` queda fuera de esa comparación junto con las tablas sombra de FTS5, y el test lo dice). **No se llama `schemaVersion`**: ese nombre es de los artefactos, donde significa lo contrario (ventana de compatibilidad y migración real).

Tres cosas que se decidieron contra la evidencia y no contra la intuición: los pragmas viajan en el **DSN como URI `file:`**, porque son por conexión (una conexión del pool sin `foreign_keys` deja de aplicar las cascadas) y porque concatenar `path+"?_pragma=…"` crea la base en otra ruta si el directorio del proyecto lleva un `?`; el bootstrap es **transaccional** (DDL y estampado juntos), que es lo que permite confiar en la generación sin inspeccionar tablas; y reconstruir **borra los archivos** en vez de las tablas, porque FTS5 crea tablas sombra que este paquete nunca escribió.

`LocalDB` no expone accesores por tabla a propósito: la forma de cada API la conoce la tarea que consume la tabla. `project_lock` es un **registro y no un mutex** — el mutex es un lockfile (`03-arquitectura.md:32`), y adquisición, caducidad y ruptura son de T-031 junto con la política que esta tarea no tenía base para inventar. El residual de T-020 sigue vivo: nada serializa todavía dos escrituras al mismo artefacto JSON.

Próximas tareas: **T-022** (indexador documental determinístico, depende de T-021 y desbloqueada por ella), **T-031** (worktrees y ramas, depende de T-030), **T-041** (motor de permisos, depende de T-040) y **T-042** (notificaciones, depende de T-021), paralelizables entre sí. T-031 hereda un aviso explícito de T-030: en cuanto empiecen a viajar nombres de rama y rutas influidos por el usuario hacia los argumentos de git, hace falta disciplina de `--` y de pathspec — el array de argumentos impide la inyección de shell, no la confusión de un valor con una opción. Riesgo prioritario a despejar temprano: spike de T-053 (arbitraje de permisos vía hooks de Claude Code) antes de congelar el diseño fino de T-050.
