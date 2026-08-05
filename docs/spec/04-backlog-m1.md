# Backlog Semilla — Implementación M1

Tareas ordenadas por dependencia. Cada bloque depende de los anteriores; dentro de un bloque, las tareas marcadas ∥ son paralelizables entre sí. Los estados siguen el ciclo de vida definido (nacen en construcción; este backlog las deja `ready_for_dev` salvo indicación).

**Criterio de cierre de M1**: un desarrollador instala la CLI, ejecuta `init`, recorre el flujo de inicialización completo por CLI (etapas omitibles, agente único por etapa vía Claude Code), y obtiene un monorepo con documentación cerrada, backlog semilla, estructura de gestión y scaffold — clonable por otra persona que con `sync` retoma el estado íntegro.

---

## Bloque 0 — Fundaciones del repositorio

**T-001 · Bootstrap del repositorio** *(estado: **completada**)*
Módulo Go, estructura de paquetes (`core/`, `store/`, `gitops/`, `providers/`, `flows/`, `api/`, `cli/`, `web/`), linters, formateo, verificación de dependencias unidireccionales (test de imports que falla si `core` importa algo o si alguien importa `cli`/`api` — regla precisada por ADR-016 al implementarla, ver abajo).

Decisiones de cierre: módulo `github.com/cburgosro9303/atrio` (a renombrar cuando se elija la organización GitHub — sub-tarea residual de T-003; el path aparece en `go.mod`, `Makefile` y `.golangci.yml`, el rename los toca los tres); Go fijado en `1.26` sin directiva `toolchain`; `web/` aún no lleva `//go:embed`: sin build de la SPA, la directiva rompería la compilación.

**Regla de dependencias resuelta por ADR-016.** Implementar la regla como test destapó que "nadie importa `cli` ni `api`" no podía convivir con la topología de ADR-002 (el comando de portal levanta el servidor desde `cli`). Se conceden dos excepciones nominales — `cmd/atrio → cli` y `cli → api` — y ninguna más; `cmd/atrio` alcanza `api` solo transitivamente. El test separa import directo de cierre transitivo para que la segunda excepción no arrastre a la primera. **Esto desbloquea T-080**, que ya no arranca contra una regla que lo prohibía.

**Insumos para T-002**: `CGO_ENABLED=0` solo en targets de build (fuera de darwin, `-race` exige cgo); `make test` con `-count=1` porque la caché de Go no ve la salida del subproceso `go list` del que depende el test de arquitectura; usar `make fmt-check`, no `make fmt`, como puerta de CI — `fmt` muta archivos y siempre sale 0.

**T-002 · CI base** *(estado: **completada**, depende de T-001)*
Pipeline: build multiplataforma (Linux/macOS/Windows), tests con `-race`, lint. Matriz de cross-compile desde el día uno para detectar dependencias que la rompan (p.ej. CGo accidental).

Decisiones de cierre: GitHub Actions como proveedor (`.github/workflows/ci.yml`) — no estaba nombrado en ninguna espec, se elige por coherencia con ADR-009, que ya fija GitHub Releases como canal de distribución. Tres trabajos: `quality` (fmt-check, vet, lint y comprobación de `tidy`, un solo runner), `test` (suite con `-race` en ubuntu/macos/windows, `fail-fast: false`) y `cross-compile`. Disparadores: `push` a `main` y a `task/**` y `bug/**` —así una tarea obtiene su evidencia antes de proponerse— más `pull_request`.

**Matriz ratificada.** La lista `linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64` queda confirmada como la matriz oficial, y `PLATFORMS` del Makefile como su única definición: el trabajo de cross-compile invoca `make build-all` en lugar de reescribir la lista en YAML, de modo que no existe una tercera copia que pueda derivar. El comentario del Makefile que remitía a T-002 como fuente autoritativa se invierte en este mismo cambio.

**`internal/citest`.** El paso de tests no puede llamar a `make test` (no hay GNU make en el runner de Windows), así que deletrea el comando — y esa copia a mano es justo la clase de deriva que el proyecto sincroniza con un test, no con un comentario. El paquete nuevo impone dos reglas leyendo Makefile y workflow como texto, sin dependencias añadidas. Primera: el comando del paso `Race suite` debe ser idéntico al recipe del target `test` — quitar `-race` o `-count=1` de cualquiera de los dos rompe el test. Segunda, enunciada en ambas direcciones: el paso de cross-compile debe invocar `make build-all` **y** el workflow no puede nombrar ninguna plataforma de `PLATFORMS`; solo prohibir los nombres dejaría pasar el caso más ruidoso, borrar el trabajo entero. Todo falla en cerrado: paso renombrado, recipe ausente o `run:` convertido a escalar de bloque abortan en vez de pasar en vacío. Verificado contra diez escenarios de deriva en una copia desechable, incluida la revisión cruzada del `code-reviewer`.

Otras decisiones dentro del alcance: acciones ancladas por SHA de commit (una etiqueta se puede mover, un SHA no) con `.github/dependabot.yml` acotado a `github-actions` para que los anclajes no se pudran — el ecosistema `gomod` se deja fuera a propósito, la dependencia de linter arrastra unos doscientos módulos indirectos. `permissions: contents: read` en la raíz del flujo: nada aquí publica. El trabajo de tests llama a `go test` directo en vez de `make test` porque el runner de Windows no trae GNU make (sí trae gcc 15.2, que es lo que `-race` necesita allí); las tres patas ejecutan el mismo comando. Se añade la comprobación de `go.mod`/`go.sum` limpios tras `make tidy`: la deriva ahí es por donde entra una dependencia sin revisar.

Pendiente heredado: el gate del invariante de almacenes (T-023) se suma al trabajo de tests cuando exista; hoy no hay nada que ejecutar. No se toca firma de binarios ni provenance: no hay requisito en ninguna espec y su lugar natural es T-081.

**T-003 ∥ · Decisión de nombre del producto** *(estado: **resuelta** — ADR-015)*
Nombre: **Atrio**. Comando `atrio`, carpeta `.atrio/`, base local `atrio.db`. Sub-tarea residual antes del primer release público: validación de marca y elección de organización GitHub distintiva (colisión conocida: Atrio Inc./atrio.io).

---

## Bloque 1 — Contratos de datos

**T-010 · JSON Schemas literales de artefactos**
Tarea, decisión, entrada de bitácora, changelog, configuración de proyecto, declaración de agentes/personalización/permisos, progreso de flujo, front-matter documental. Incluye `schemaVersion` en todos y las reglas de validación especiales (estados definitivos inmutables, `artifactLanguage` inmutable, `blockedBy` obligatorio en bloqueada, unicidad de `displayName`).

**T-011 ∥ · JSON Schema del manifiesto del marketplace**
`manifestVersion`, items con `type: agent|skill|pack|flow`, descripciones `{es,en}`, `requiredCapabilities`, checksums. **API pública: revisión extra antes de congelar.**

**T-012 ∥ · Esquema de definición canónica de agente y de flujo declarativo**
Formato agnóstico de agente (incluida la sección de personalización delimitada) y formato de flujo (etapas, participantes por rol, entradas, salidas con esquema, omitibilidad, checklist de datos bloqueantes).

---

## Bloque 2 — Capa de persistencia (`store/`)

**T-020 · Repositorio de artefactos JSON** *(depende de T-010)*
Lectura/escritura con validación contra esquema, un archivo por entidad, IDs ULID, atribución `createdBy` desde identidad git. Rechazo/marcado de artefactos inválidos con error reparador (campo, formato).

**T-021 · SQLite local** *(depende de T-020)*
Esquema de base local: índice documental, FTS5, notificaciones, locks, sesión, hashes de materialización. Bootstrap y regeneración completa desde el repo.

**T-022 · Indexador documental determinístico** *(depende de T-021)*
Parser de front-matter, validación, construcción del índice + FTS. Detección de documento inválido con rastreo de autor (git blame del cambio) y notificación de impacto.

**T-023 · Test del invariante de almacenes** *(depende de T-021, T-022)*
`borrar db + clonar + sync` reconstruye el 100% del estado local. En CI.

---

## Bloque 3 — Git (`gitops/`)

**T-030 · Envoltura segura de git** *(depende de T-001)*
Detección de binario y versión mínima, invocación por arrays (sin shell), parseo `--porcelain`. Validación/solicitud de identidad (user.name/email) con bloqueo al primer uso.

**T-031 · Gestión de worktrees y ramas** *(depende de T-030)*
Creación `{task|bug}/{ulid-corto}-{slug}`, worktree por tarea, limpieza determinística de huérfanos, commit/push gobernados.

**T-032 · Generador de changelog versionado** *(depende de T-031, T-020)*
Generación al preparar push; sugerencia de título/descripción de PR; lectura tras pull para actualización de backlog.

---

## Bloque 4 — Dominio (`core/`)

**T-040 · Entidades y máquinas de estado** *(depende de T-010)*
Ciclo de vida de tarea completo (construcción → ready_for_dev → ejecución → bucle por ambiente → definitivos; bugs desde triage), decisiones inmutables con reemplazo, bitácora append-only. Sin I/O: lógica pura testeable.

**T-041 · Motor de permisos** *(depende de T-040)*
7 categorías × 3 niveles, perfiles predefinidos, expansión perfil→mapa, arbitraje de solicitudes de autorización, registro de autorizaciones en bitácora, regla de no-acumulación especulativa.

**T-042 ∥ · Sistema de notificaciones interno** *(depende de T-021)*
Niveles/filtros, persistencia local, salida CLI en M1.

---

## Bloque 5 — Proveedores (`providers/`)

**T-050 · Interfaz del adaptador + registro** *(depende de T-012, T-041)*
Las 5 responsabilidades, catálogo cerrado de capacidades, stream de eventos normalizados, rangos de versión soportada. Contrato validado contra convenciones documentadas de Copilot y Antigravity (revisión de diseño, sin implementarlos).

**T-051 · Adaptador Claude Code: detección + capacidades** *(depende de T-050)*

**T-052 · Adaptador Claude Code: materialización + deriva** *(depende de T-051)*
Compilación canónico→`.claude/`, hashes, comparación tres vías (modificado/faltante/huérfano), resolución sobrescribir/conservar/adoptar.

**T-053 · Adaptador Claude Code: ejecución headless** *(depende de T-051)*
Supervisor efímero, traducción de salida a eventos normalizados, transporte de permisos vía hooks/modo de aprobación, métricas de tokens si disponibles.

**T-054 · Adaptador Claude Code: sesión conversacional** *(depende de T-053)*
Stream bidireccional, un agente (M1).

**T-055 · Contramedidas del core** *(depende de T-050)*
Estrategias de degradación por capacidad como código del core (mínimo: `skills → inyección en prompt`; `hooks → advertir y omitir`).

---

## Bloque 6 — Distribución de definiciones y sync

**T-060 · Repositorio público de definiciones** *(depende de T-011, T-012, T-003)*
Estructura del repo GitHub de agentes/flujos/packs, manifiesto por tag, checksums, definiciones canónicas de los 4 agentes M1 (idea-explorer, product-owner, architect, project-manager) y del flujo de inicialización declarativo.

**T-061 · Caché global + comando `sync`** *(depende de T-060, T-052)*
Descarga por tag con verificación de integridad y fuente fijada, caché por versión, materialización desde declaración del proyecto, reporte de deriva y de lo no materializable, sugerencia de sync tras pull.

---

## Bloque 7 — Motor de flujos (`flows/`)

**T-070 · Motor genérico de flujos declarativos** *(depende de T-040, T-054)*
Carga de definición, máquina de estados por etapa (pendiente/en_curso/pendiente_de_cierre/cerrada/omitida), progreso versionado, reanudación tras interrupción, moderador programático con agenda (un participante en M1).

**T-071 · Extracción al cierre** *(depende de T-070, T-020)*
Salida estructurada del agente validada contra esquema, reintento acotado, `pendiente_de_cierre` + notificación en fallo, aprobación humana configurable.

**T-072 · Checklist de datos bloqueantes + preguntas de cierre** *(depende de T-070)*
Cómputo de faltantes, secuencia de preguntas directas solo por huecos.

**T-073 · Ingesta de documento inicial** *(depende de T-070)*
Ruta "aporto un documento": extracción de lenguajes/tecnologías/definiciones identificables para pre-poblar el flujo.

**T-074 · Generador de scaffold** *(depende de T-072)*
Etapa final determinística: estructura de capas según definición bloqueante, semillas documentales, configuración de gestión, `.gitignore` con el `.db`, commits por etapa a `main`.

---

## Bloque 8 — CLI y cierre de M1

**T-080 · Comandos de la CLI** *(depende de bloques 5–7)*
`init` (flujo completo), `sync`, gestión de tareas (listar/mover estado/detalle), gestión de permisos de agentes, notificaciones, reporte de tokens de sesión.

**T-081 · Pipeline de release** *(depende de T-002, T-003)*
GitHub Releases multiplataforma, Homebrew tap, winget, script de instalación Linux, esqueleto del comando `upgrade` (la migración completa de esquemas llega con el primer cambio de esquema real).

**T-082 · Documentación pública** *(depende de T-080)*
README (qué es, quick start < 5 min), guía de contribución, publicación de JSON Schemas como contrato, declaración de la ventana de retrocompatibilidad, prerrequisitos (git, CLI de proveedor).

**T-083 · Prueba de humo end-to-end** *(depende de todo)*
Escenario del criterio de cierre: init → flujo completo → scaffold → clone en segunda máquina → sync → estado íntegro. Automatizada donde sea posible; checklist manual para la parte conversacional.

---

## Riesgos de implementación a vigilar

1. **T-053 es la tarea de mayor incertidumbre**: el transporte de permisos vía mecanismos de Claude Code depende de capacidades de terceros que evolucionan. Hacer *spike* técnico temprano (antes de cerrar el diseño fino de T-050) para validar viabilidad del arbitraje en el core.
2. **T-011 congela API pública**: revisar dos veces antes del primer tag.
3. **Cross-compile limpio**: cualquier dependencia con CGo lo rompe; la matriz de CI (T-002) lo detecta desde el inicio.
4. **Marca del nombre Atrio**: la colisión con Atrio Inc. (atrio.io) debe validarse legalmente antes del primer release público; el naming técnico (`atrio`, `.atrio/`) ya está fijado.
