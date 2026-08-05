# Registro de Decisiones Técnicas (ADRs)

**Documento vivo** · Sesión de definición técnica y arquitectónica
Estado: bloques (a) adaptador de proveedores y (b) esquemas de artefactos cerrados · bloque (c) motor de flujos en curso
Complementa a: *Definición de Producto — Plataforma de Desarrollo Asistido por Agentes*

Formato: cada ADR registra contexto, decisión, alternativas evaluadas y consecuencias. Los ADRs son inmutables; un cambio futuro se registra como nuevo ADR que reemplaza al anterior.

---

## ADR-001 — Runtime del núcleo: Go

**Contexto.** La decisión de mayor costo de cambio. Drivers: multiplataforma, núcleo único CLI+portal, orquestación de procesos externos (CLIs de agentes), git intensivo, comunidad open source contribuyente, eficiencia (arranque rápido, bajo consumo), mantenibilidad a años.

**Alternativas evaluadas.** TypeScript/Node (mayor comunidad del nicho, unifica frontend; menor robustez y eficiencia), Rust (máxima seguridad de memoria y rendimiento CPU; curva alta, iteración lenta, comunidad contribuyente menor, ciclos de agente más caros), Java/GraalVM (máximo dominio del mantenedor; fricción de native-image y comunidad pequeña en el nicho), Go.

**Decisión.** **Go.** Sistema I/O-bound donde la ventaja de Rust es irrelevante; Go domina en concurrencia para supervisión de agentes (goroutines/channels), es el estándar de facto del nicho CLI dev-tools (kubectl, terraform, gh, docker → base de contribuidores máxima), compila a binario único estático con cross-compile trivial, tiene compatibilidad 1.x legendaria, y es de los lenguajes donde los agentes de IA (que construirán gran parte de la plataforma) rinden mejor: corpus dominante en el dominio exacto, una sola forma idiomática, ciclos editar→compilar→probar de segundos.

**Consecuencias.** Repositorio bilingüe: Go (core) + TypeScript (SPA). Frontend inevitablemente separado del lenguaje del core. Data races posibles → `-race` obligatorio en CI.

---

## ADR-002 — Topología: ejecutable único, sin daemon, disco como fuente de verdad

**Contexto.** Paridad de capacidades CLI/portal prohíbe lógica duplicada; retomabilidad exige que ningún proceso sea dueño del estado.

**Decisión.** Un único ejecutable con el core como biblioteca interna. La CLI es la puerta de entrada; el comando de portal levanta un servidor HTTP local que sirve la SPA embebida y expone la misma API interna que consume la CLI. Cero lógica en el frontend más allá de presentación. Sin base de datos servidor ni daemon obligatorio: el repositorio es la fuente de verdad del estado compartido; cualquier proceso puede morir y el estado sobrevive. Ejecuciones largas de agentes: proceso supervisor efímero por ejecución. Concurrencia entre CLI, portal y agentes paralelos arbitrada por locks por proyecto (lockfiles). Todo artefacto lleva número de versión de esquema desde el día uno.

**Consecuencias.** Habilita el protocolo de migraciones ya definido funcionalmente; simplifica distribución (un artefacto); la API HTTP local queda como contrato reutilizable por integraciones futuras.

---

## ADR-003 — Integración git: delegación al binario del sistema

**Contexto.** Git es columna vertebral (worktrees, ramas, atribución). Las bibliotecas embebidas de git suelen tener soporte incompleto justamente de worktrees.

**Decisión.** Git del sistema como prerrequisito declarado de la plataforma (igual que las CLIs de proveedores). El core lo envuelve con una capa propia que: (a) nunca interpola shell — invocación por array de argumentos (prioridad de seguridad), (b) parsea salidas estables (`--porcelain`).

**Consecuencias.** Comportamiento idéntico al que el desarrollador ve en su terminal; cero divergencia de implementación; dependencia de versión mínima de git a declarar.

---

## ADR-004 — Primer proveedor: Claude Code, con contrato preparado para N proveedores

**Contexto.** M1 implementa un solo adaptador; el agnosticismo es la promesa central.

**Decisión.** Claude Code como primer proveedor: convenciones más ricas y documentadas para el caso de uso (carpeta `.claude/` por proyecto, agentes/subagentes, skills, hooks, modo headless con salida estructurada). Requisito verificable: agregar un proveedor = implementar la interfaz del adaptador + registrarlo; cero cambios en core, flujos, comandos. La interfaz se diseña validada contra las convenciones documentadas de Copilot y Antigravity aunque solo se implemente un adaptador.

---

## ADR-005 — Formato de artefactos: JSON + JSON Schema

**Contexto.** La validación determinística estricta (documento válido = indexable; rastreo de estructura rota) es regla de negocio.

**Decisión.** JSON con JSON Schema publicado como contrato open source. Versionado de esquema embebido en cada artefacto. El manifiesto del marketplace sigue el mismo estándar.

**Alternativas.** YAML descartado: su laxitud (tipos implícitos, ambigüedades) juega contra la validación estricta.

---

## ADR-006 — Tres almacenes de datos

**Contexto.** Tensión resuelta: separar datos de la plataforma de documentos del proyecto sin romper la portabilidad por repositorio. Un binario versionado en git no se mergea ni se diffea: rompería el modelo de equipo (conflictos git normales), el changelog como puente de sincronización y la auditoría por atribución.

**Decisión.**

| Almacén | Contenido | Viaja en el repo |
|---|---|---|
| Artefactos JSON versionados (carpeta de gestión) | Tareas, decisiones, bitácora, changelogs, configuración, declaración de agentes, personalización, permisos | ✅ |
| SQLite local (directorio de datos de la plataforma por proyecto, en `.gitignore` generado por `init`) | Índice documental (regenerable), caché de metadatos, sesión en curso, notificaciones, locks, consumo de tokens de la sesión, registro de ejecuciones en vivo | ❌ |
| Documentos legibles (markdown, carpeta docs) | Corpus del proyecto del desarrollador (PRD, visión, análisis) | ✅ |

**Invariante arquitectónico verificable (test en CI):** `borrar SQLite + clonar el repo + sync` reconstruye el 100% del estado local. Todo dato que deba sobrevivir esa prueba y no lo haga está en el almacén equivocado.

---

## ADR-007 — Base embebida: SQLite

**Contexto.** El almacén local necesita consultas por metadatos/relaciones para el índice documental y búsqueda de texto (habilita consultas Q&A y análisis sin cargar el corpus). Un solo escritor por proyecto (locks ya arbitran).

**Alternativas.** bbolt y BadgerDB (clave-valor: sin consultas ni FTS — el índice degeneraría en estructuras ad-hoc), DuckDB (analítico columnar, CGo rompe cross-compile), solo JSON + índice en memoria (penaliza arranque, búsqueda a mano).

**Decisión.** SQLite 3.x con driver Go puro `modernc.org/sqlite` (sin CGo, cross-compile limpio). FTS5 para búsqueda full-text documental; SQL para el grafo de metadatos. Pinning fino de versión en las dependencias del build.

---

## ADR-008 — Frontend del portal: React + TypeScript + Vite, SPA embebida

**Contexto.** Criterios: comunidad contribuyente, calidad de código generado por agentes, madurez a años, complejidad del board interactivo y mesas en vivo. El tamaño de bundle es irrelevante (servido desde localhost, embebido en el binario).

**Alternativas.** Vue (alternativa razonable, curva más suave), Svelte (sin ventaja decisiva; menor estabilidad de API tras transición 4→5).

**Decisión.** React (línea 19.x) + TypeScript + Vite (línea 8.x), embebido en el binario Go — un solo artefacto distribuible. Versionado por líneas mayores en este ADR; pinning exacto en el lockfile.

---

## ADR-009 — Distribución: GitHub Releases + canales nativos + upgrade integrado

**Decisión.** Binarios por plataforma en GitHub Releases; instaladores por canal nativo (Homebrew, winget, script para Linux); comando `upgrade` integrado que respeta el protocolo de migraciones (notificación previa, migración por proyecto posponible, retrocompatibilidad de lectura por ventana de versiones).

---

## ADR-010 — Comunicación portal↔core: HTTP+JSON con token, SSE para tiempo real

**Decisión.** HTTP + JSON local con el token de sesión generado por la CLI al levantar el portal (blindaje ya definido: localhost-only, sin destructivos por GET). SSE (Server-Sent Events) para el flujo en vivo (actividad de agentes, notificaciones): unidireccional, más simple que WebSockets y suficiente — los comandos del portal viajan por HTTP normal.

---

## ADR-011 — Estructura del monorepo generado por `init`

**Decisión.** Raíz del proyecto: `docs/` (corpus legible del desarrollador); carpeta oculta de plataforma con `config.json`, `agents.json` (declaración + personalización + permisos), `management/` (tareas, decisiones, bitácora, changelogs — un archivo JSON por entidad) y el `.db` local ignorado; carpetas de capas (`frontend/`, `backend/`, `iac/`, …) creadas solo tras la definición bloqueante de stack; carpetas de proveedores (`.claude/`, …) en la raíz porque sus CLIs las esperan ahí. Naming exacto pendiente del nombre del producto.

---

## ADR-012 — Descomposición del core Go en paquetes con dependencias unidireccionales

**Decisión.** `core/` (dominio: tareas, decisiones, flujos — sin I/O), `store/` (JSON versionado + SQLite + validación de esquemas), `gitops/` (envoltura segura de git/worktrees), `providers/` (interfaz de adaptador + `claudecode/`), `flows/` (motor de etapas, reanudable), `api/` (HTTP+SSE local), `cli/` (comandos), `web/` (SPA embebida). Reglas: `core` no importa ningún otro paquete; nadie importa `cli` ni `api`.

**Estado.** La descomposición en paquetes sigue vigente. La **regla de dependencias queda reemplazada por ADR-016**, que la precisa con dos excepciones acotadas y ejecutables.

---

## ADR-013 — Contrato del adaptador de proveedores

**Contexto.** Corazón de la promesa de agnosticismo. El core nunca conoce un proveedor; conoce el contrato.

**Decisión.** Cinco responsabilidades:

1. **Identidad y descubrimiento**: detectar CLI instalada, versión, compatibilidad mínima.
2. **Declaración de capacidades**: contra un **catálogo cerrado gobernado por la plataforma** (`agentes`, `skills`, `hooks`, `ejecución_headless`, `salida_estructurada`, `sesión_interactiva`, `reporte_de_tokens`, `contexto_por_archivo`, …). Alimenta contramedidas, matriz visible en marketplace y bloqueos de instalación.
3. **Materialización** (`sync`): compila definiciones canónicas al dialecto nativo del proveedor y las escribe en su carpeta; reporta deriva entre lo declarado y lo materializado.
4. **Ejecución**: dos modos — tarea headless y sesión conversacional — ambos emitiendo un **stream de eventos normalizados** (inicio, texto, uso de herramienta, solicitud de autorización, fin, error, métricas). **Los permisos se arbitran en el core**: el adaptador reporta "el agente quiere ejecutar X" como evento; el core decide según la configuración de las 7 categorías y responde. Idéntico comportamiento en todos los proveedores. En M1, el transporte de estos eventos usa los mecanismos de intercepción de Claude Code (hooks/modo de aprobación).
5. **Métricas**: tokens aproximados por ejecución si el proveedor los expone (capacidad opcional; si no existe se muestra "no disponible" — nunca se estima inventando).

**Definición canónica agnóstica**: formato propio JSON, compilado por cada adaptador (vs. mantener N formatos nativos — descartado por multiplicar mantenimiento y romper el agnosticismo).

**Contramededidas**: estrategias de degradación por capacidad, registradas como **código del core** (p.ej. `skills → inyección en prompt`; `hooks → advertir y omitir`), nunca decisión del adaptador — degradación uniforme garantizada.

**Alcance M1**: detección, matriz, materialización + deriva, ejecución headless, sesión conversacional de un agente. Mesas multi-agente en M2 como extensión de uso del mismo contrato, sin cambiar la interfaz.

**Versiones de proveedores**: cada adaptador declara rango de versiones soportadas de su CLI; fuera de rango la plataforma avisa y degrada a "no disponible" en lugar de fallar impredeciblemente. Tests del adaptador contra versiones fijadas en CI.

---

## ADR-014 — Esquemas de artefactos (diseño conceptual)

**Sobre común**: `schemaVersion`, `id`, `createdAt/updatedAt`, `createdBy` (identidad git o nombre de agente).

**IDs: ULID** — únicos sin coordinación (creación offline concurrente sin colisión), ordenables cronológicamente (la bitácora se ordena por nombre de archivo), legibles.

**Tarea** (`type: task | bug`): `title`, `description`, `epicId?`, `tags[]`; `state` + `stateHistory[]` embebido (`{de, a, quién, cuándo, motivo?}` — auditoría autocontenida y portable; la bitácora registra eventos de ejecución, la tarea su propia historia, sin duplicación); `blockedBy[]` tipado (tarea | pregunta_al_usuario | decisión_pendiente), obligatorio en estado bloqueada; `assignee?`; `closureEnvironment`; `environmentProgress[]` con `confirmedBy` humano por entrada; `branch?/worktree?` (una tarea = una rama); `changelogRefs[]`. Bugs nacen `triage` con `originTaskRef?`. Estados definitivos rechazan mutación salvo referencias entrantes.

**Decisión**: `title`, `context`, `decision`, `consequences[]`, `alternativesConsidered[]?`; `status: activa | reemplazada` + `supersededBy?`; inmutable salvo la transición a reemplazada; `refs[]`.

**Bitácora**: un archivo por entrada con id ULID ordenable (append-only real en git, colisiones imposibles); `timestamp`, `actor`, `eventType` de catálogo cerrado (incluye `autorización_otorgada/denegada` con qué, a qué agente, por quién), `payload` tipado, `refs[]`.

**Changelog**: `taskRefs[]`, `branch`, `summary`, `changes[]` (`{ruta, tipo, descripción}`), `impacts[]?` (documentos/definiciones afectadas — lo que los agentes leen tras pull).

**Configuración de proyecto**: `name`, `artifactLanguage` (inmutable, validado), `environments[]` (mutable, default `dev, staging, prod`), `layers[]`, `providers[]`, `platformVersion`, `schemaVersions` (mapa artefacto→versión para migraciones selectivas).

**Declaración de agentes**: `packs[]/agents[]` como `id@versión` contra el caché global; `personalization` por agente (`displayName` con unicidad validada + sección de personalización); `permissions` por agente (`profile` predefinido o mapa explícito de las 7 categorías; el mapa explícito prevalece).

**Metadatos documentales**: front-matter estructurado al inicio del propio documento markdown (título, propósito, etiquetas, relaciones, idioma). Un solo archivo viaja completo; imposible desincronizar metadato y contenido (sidecar descartado). El validador parsea el front-matter; el índice SQLite se construye de ahí.

**Manifiesto del marketplace** (API pública, máximo costo de cambio): `manifestVersion`, `platformVersion` (tag); `items[]` con `{id, type: agent|skill|pack, name, description: {es, en} ambos obligatorios, providers | "all", requiredCapabilities[], files[], checksums, stage/rol}`. Los packs agrupan por composición (por lenguaje/dominio), extensibles sin cambiar el esquema.

---

## ADR-015 — Nombre del producto: Atrio

**Contexto.** El nombre condiciona el comando de la CLI, la carpeta oculta de plataforma, el repositorio público y la identidad open source. Criterios: corto y tecleable, pronunciable igual en español e inglés, evocador (el atrio como espacio central donde todo converge — coherente con el monorepo como centro del proyecto y las mesas de agentes).

**Decisión.** **Atrio**. Comando: `atrio` (`atrio init`, `atrio sync`). Carpeta de plataforma: `.atrio/`.

**Riesgo documentado (diligencia debida).** Existe Atrio Inc. (atrio.io), empresa con plataforma de computación para desarrolladores (Kubernetes/HPC) con CLI propia y organización GitHub `atrioinc`; también el videojuego "Atrio: The Dark Wild". Ninguna colisión bloquea técnicamente el comando ni la carpeta, pero antes del primer release público debe validarse el aspecto de marca y elegirse un nombre de organización GitHub distintivo (p.ej. `atrio-dev` o similar).

---

## ADR-016 — Excepciones acotadas a la regla de dependencias unidireccionales

*Reemplaza la regla de dependencias de ADR-012 (la descomposición en paquetes de ADR-012 sigue vigente).*

**Contexto.** ADR-012 fija "nadie importa `cli` ni `api`". Al implementarla como test ejecutable en T-001 aparecen dos puntos donde la regla literal es inviable, no por comodidad sino por la topología que ADR-002 ya había decidido:

1. Un binario único necesita un `package main`, y ese main necesariamente importa `cli`.
2. ADR-002 establece que "el comando de portal levanta un servidor HTTP local que sirve la SPA embebida y expone la misma API interna que consume la CLI". Ese comando vive en `cli` y arranca el servidor de `api`. La regla literal y ADR-002 no pueden ser ambas ciertas.

Detectar esto al escribir el test —y no al llegar a T-080— es exactamente para lo que sirve hacer ejecutable una regla arquitectónica.

**Decisión.** La regla se mantiene con dos excepciones nominales, y solo dos:

- **`cmd/atrio` puede importar `cli`** y, de las capas de entrega, únicamente `cli`. No es un consumidor de la capa de entrega: *es* la capa de entrega, que es lo que la regla protege aguas abajo.
- **`cli` puede importar `api`**, para que el comando de portal levante el servidor. Es la dirección que ADR-002 ya implicaba.

Ambas excepciones se conceden por árbol, no por paquete exacto: los subpaquetes de `cli` cuentan como `cli`, y los de `cmd/atrio` heredan su permiso sobre `cli`. Es deliberado — el arranque del portal puede vivir en `cli/internal/portal` sin pedir un ADR nuevo. Lo que **no** se hereda es el acceso directo a `api`: `cmd/atrio` y cualquier subpaquete suyo lo tienen prohibido.

De la segunda se sigue que `cmd/atrio` alcanza `api` **transitivamente** a través de `cli`. Eso es consecuencia de la excepción concedida, no una tercera excepción: `cmd/atrio` **no** puede importar `api` directamente, y el test distingue ambos casos usando imports directos y cierre transitivo por separado.

Lo que sigue prohibido, sin excepción: `core` importando cualquier paquete del módulo, y cualquier otro paquete (`store`, `gitops`, `providers`, `flows`, `web`) alcanzando `cli` o `api` por cualquier vía, directa o transitiva.

**Alternativas evaluadas.** *(a) `func main()` dentro de `cli/`* — cumple la regla al pie de la letra sin excepciones, pero es no idiomático y vuelve `cli` un `package main` no importable por nada, incluidos tests de integración futuros. *(b) Extraer un paquete intermedio que `cli` y `api` compartan y que `cli` use para arrancar el servidor* — elimina la excepción a costa de un paquete que existe solo para satisfacer la regla, y desplaza el acoplamiento sin reducirlo. *(c) Mantener la regla literal y resolverlo en T-080* — descartada: deja la contradicción latente hasta el punto de mayor coste de cambio.

**Consecuencias.** La regla deja de ser prosa y pasa a ser un test que discrimina import directo de cierre transitivo (`internal/archtest`). Las excepciones son nominales y aparecen en el código con su justificación: no son un `if` genérico que se pueda ensanchar sin que se note en el diff. Toda excepción futura exige un ADR nuevo, no una edición al test.

---

## ADR-017 — Front-matter documental en YAML validado

*Acota el alcance de ADR-005, que sigue vigente sin excepción para los artefactos de la plataforma.*

**Contexto.** ADR-005 descarta YAML como formato de artefactos por su laxitud: tipos implícitos y ambigüedades que juegan contra la validación estricta. ADR-014 fija que los metadatos documentales viven en front-matter dentro del propio markdown —el sidecar se descartó porque metadato y contenido se desincronizan— pero no declara en qué se serializa ese bloque. Los documentos no son artefactos de la plataforma: los escribe un humano en su editor, los renderiza GitHub, y el índice determinístico se construye parseándolos. La convención universal del ecosistema markdown para ese bloque es YAML entre `---`, y es la que esperan editores, previsualizadores y el propio GitHub.

**Decisión.** Front-matter en **YAML entre delimitadores `---`**, restringido a un subconjunto plano: sin anclas, sin alias, sin tags personalizados, sin documentos múltiples. El bloque se parsea y el resultado se valida contra `document-front-matter.schema.json` antes de indexar. La laxitud que ADR-005 le reprocha al YAML se ataca donde se puede atacar de verdad —validando lo que salió del parseo— y no eligiendo un formato que nadie escribe a mano dentro de un markdown.

El alcance de esta acotación es exactamente ese bloque. Los artefactos de la plataforma (`.atrio/**/*.json`) siguen siendo JSON sin excepción alguna.

**Alternativas evaluadas.** *(a) Bloque JSON delimitado* — coherencia total con ADR-005 y un solo parser en toda la plataforma, pero rompe la convención que espera cualquier herramienta markdown y obliga al humano a escribir comillas y comas sin comentarios, justo en el paso que decide si su documento es indexable: empuja al error precisamente donde más caro sale. *(b) TOML entre `+++`* — tipado explícito, sin las ambigüedades del YAML y con convención establecida por Hugo, pero suma un tercer formato al proyecto y su reconocimiento fuera del ecosistema Hugo es desigual. *(c) Sidecar* — ya descartada por ADR-014 por desincronización.

**Consecuencias.** La ambigüedad clásica del YAML deja de ser silenciosa: un `language: no` que el parser convierte en booleano —el "problema noruego"— ya no se cuela como valor plausible, choca contra un esquema que exige cadena y produce un error reparador que nombra campo y formato. Un front-matter que no parsea y uno que no valida son indistinguibles para el usuario y caen por la misma ruta: documento no indexable, rechazado o marcado, con rastreo de autor por atribución git.

La elección de la biblioteca YAML —Go puro, sin CGo— pertenece a T-022, donde vive el parser del indexador. T-010 no la necesita: el esquema valida la forma ya parseada, y sus fixtures son esa forma.

Toda ampliación futura del alcance de esta acotación —otro formato, otro lugar donde se acepte YAML— exige un ADR nuevo, no una lectura extensiva de este.

---

## Pendientes de la sesión técnica

Los pendientes de la versión anterior de este documento (motor de flujos, convención de ramas, catálogo de agentes, deriva de sync, ventana de retrocompatibilidad, nombre del producto) fueron resueltos y consolidados en el *Documento de Arquitectura*. Quedan abiertos únicamente:

- JSON Schemas literales del marketplace y de las definiciones canónicas (T-011/T-012). Los de los artefactos de gestión los cerró T-010.
- Política fina de disparadores de actualización documental (F4).
- Validación de marca del nombre Atrio antes del primer release público (ADR-015).
