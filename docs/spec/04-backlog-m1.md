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

**T-010 · JSON Schemas literales de artefactos** *(estado: **completada**)*
Tarea, decisión, entrada de bitácora, changelog, configuración de proyecto, declaración de agentes/personalización/permisos, progreso de flujo, front-matter documental. Incluye `schemaVersion` en todos y las reglas de validación especiales (estados definitivos inmutables, `artifactLanguage` inmutable, `blockedBy` obligatorio en bloqueada, unicidad de `displayName`).

Los nueve archivos viven en `schemas/` (paquete hoja, embebido con `go:embed`), con `README.md` como contrato legible y la tabla de qué valida el esquema y qué valida el código. La spec fijaba los campos pero casi ninguna convención; estas son las decisiones de cierre.

**Convenciones.** Draft **2020-12** —no es preferencia: la envolvente se compone con `allOf` y cada artefacto se cierra con `unevaluatedProperties: false`, combinación que draft-07 no permite—. `$id` **relativo** e idéntico al nombre del archivo: Atrio no tiene dominio propio todavía (ADR-015) y un `$id` absoluto sería una URL que el proyecto no controla; publicarlos bajo una base en T-082 no exigirá tocar los archivos. `schemaVersion` **entero** monótono por tipo, inferido de la aritmética `N`/`N-1`/`N-2` de la ventana de compatibilidad. Ningún campo desconocido pasa.

**Idioma del contrato (decisión del usuario).** La spec escribía los valores en dos idiomas —`ready_for_dev` en inglés, `completada`/`activa`/`en_curso` en español, y los subcampos de `stateHistory` como `{de, a, quién, cuándo, motivo}` con tildes—. Se congela **todo en inglés**: el español de la spec es prosa descriptiva, no elección de identificador, `ready_for_dev` era ya la única forma fijada en backticks, y CLAUDE.md manda que el bilingüismo llegue por i18n y no por traducción de código. Además saca las tildes de un contrato público.

**Modelado dentro del alcance.** El bucle por ambiente son dos estados cerrados (`deploying`, `testing`) más `currentEnvironment`, no un estado por ambiente: así el enum sigue siendo validable mientras `environments[]` permanece mutable. La entrada de bitácora **no** redeclara `timestamp` ni `actor`: la envolvente ya los aporta como `createdAt`/`createdBy` y duplicarlos abriría la puerta a que discrepen —el resumen de arquitectura ya los omitía—. El catálogo de `eventType` se cierra en seis valores (`agent_run_started`, `agent_run_finished`, `authorization_granted`, `authorization_denied`, `milestone`, `note`); la spec solo daba el de autorizaciones como ejemplo. El front-matter lleva una **envolvente reducida** (`schemaVersion`, `id`): fechas y autoría a mano en un markdown se pudren contra git, que ya las sabe. No se inventaron campos que la spec no menciona —estimación, prioridad, criterios de aceptación—: añadirlos después es aditivo y seguro; quitarlos de un contrato congelado, no. El patrón de `branch` queda deliberadamente laxo porque la longitud del ulid-corto la fija T-031.

**Contradicciones resueltas.** *Front-matter*: ADR-005 descarta YAML y ADR-014 no declaraba serialización — se resuelve con **ADR-017**, que acota (no rompe) ADR-005 al bloque de front-matter y valida la forma ya parseada. *Progreso de flujo*: `03-arquitectura.md:39` lo lista en el almacén JSON versionado pero el árbol de `:47` no lo nombra — se trata como omisión del árbol y vive en `management/flows/`. *`title` de decisión*, presente en ADR-014 y ausente del resumen de arquitectura: se incluye, ADR-014 es el listado completo.

**Pruebas.** Cada esquema compila contra el meta-esquema, cumple las convenciones (verificadas, no prometidas) y se ejerce contra sus fixtures válidas e inválidas —sin cifras aquí: un número en prosa se queda obsoleto en cuanto alguien añade una fixture, y quien las cuenta es el test—. Una fixture inválida se llama `<propiedad>--<motivo>.json` y el test no se conforma con que sea rechazada: **exige que el error apunte a esa propiedad**, recorriendo el árbol de causas del validador en vez de buscar subcadenas —una subcadena acierta por casualidad—. Esa aserción es la semilla del error reparador de T-020. Arnés probado contra seis regresiones (palabra clave inválida, `$ref` roto, `unevaluatedProperties` aflojado, `$id` desalineado, regla eliminada, esquema sin fixtures) y contra una fixture con nombre mentiroso.

**Dependencia nueva (aprobada).** `github.com/santhosh-tekuri/jsonschema/v6 v6.0.2`, Apache-2.0, Go puro sin CGo. Ya estaba en el grafo del módulo como dependencia indirecta del linter, así que promoverla a directa **no añade ningún módulo al build**: `go.sum` no cambia.

**Herencia para T-020.** Cuando falla un campo de la envolvente, el error real llega acompañado de una cascada de `false schema` sobre el resto de la envolvente —efecto de que la rama `allOf` fallida pierde sus anotaciones bajo `unevaluatedProperties`—. El error reparador debe quedarse con la causa más específica del árbol, no aplanar la lista.

**T-011 ∥ · JSON Schema del manifiesto del marketplace** *(estado: **completada**)*
`manifestVersion`, items con `type: agent|skill|pack|flow`, descripciones `{es,en}`, `requiredCapabilities`, checksums. **API pública: revisión extra antes de congelar.** El esqueleto completo se revisó campo por campo antes de escribirlo; lo que sigue son las decisiones de cierre.

**Una tercera familia de esquema.** El manifiesto no es un artefacto de proyecto: lo publica el repositorio de definiciones por tag, así que no tiene ULID ni identidad git que atribuirle, y la spec le da `manifestVersion`/`platformVersion` en vez de la envolvente común. Darle la envolvente de artefacto habría exigido inventarle ambos campos, justo lo que T-010 se prohibió. Se añade `manifestEnvelope` a `common.schema.json` —con `manifestVersion` como `$ref` a `schemaVersion`, porque ADR-005 dice que el manifiesto sigue el mismo estándar y es la generación del esquema bajo el nombre de la API pública— y **el test se endurece en vez de eximirse**: antes bastaba llevar *una* de las dos envolventes, lo que dejaba pasar un front-matter que se declarase artefacto; ahora cada esquema lleva **exactamente una y la de su familia**, y la familia por defecto es la de artefacto, así que un esquema nuevo queda sujeto a la regla hasta que alguien decida otra cosa.

**`requiredCapabilities` se valida en código, no en el enum (decisión del usuario).** `03-arquitectura.md:66` y ADR-013 enumeran ocho capacidades y cierran con `…`: el catálogo está declarado cerrado pero la spec lo deja abierto, y quien lo cierra es T-050, que corre *después* de que este contrato sea público. Lo que discrimina no es enum contra patrón sino si la novena capacidad debe bumpear `manifestVersion`. Se elige el patrón `^[a-z][a-z0-9_]*$` más pertenencia verificada por el core: añadir una capacidad pasa a ser una release de plataforma y no un cambio incompatible de una API publicada. El precedente de `permissionCategory` —enum cerrado— no aplica: aquellas siete categorías estaban completamente enumeradas en la spec.

**Contradicciones resueltas.** *`type`*: ADR-014 lista `agent|skill|pack`, pero `03-arquitectura.md:86`, `:90` y este backlog incluyen `flow`, y §9 hace de los flujos definiciones distribuidas por el marketplace — entra `flow`, ADR-014 es la lista más vieja y estrecha. *`stage/rol`*: se lee como "uno u otro según el ítem". Se incluye `role` (obligatorio en `agent`, prohibido en el resto) y **se omite `stage`**: T-012 declara los participantes de una etapa por rol, así que el binding etapa↔rol lo posee el flujo, y la tabla de agentes M1 es el mapeo *del flujo de inicialización*, no una propiedad del agente — duplicarlo se rompería en cuanto un segundo flujo reutilizara un rol. La fixture inválida `stage--unknown-property.json` deja la decisión ejecutable.

**Modelado dentro del alcance.** `files[]` y `checksums` se funden en `files[]` de `{path, sha256}`: un checksum sin archivo, o al revés, quedan irrepresentables. `sha256` y no `checksum` —un digest sin algoritmo es una migración futura garantizada—. El `path` lleva patrón restrictivo que hace **irrepresentables** los segmentos `.` y `..`: la defensa contra traversal al materializar en `.claude/` tiene que vivir en el contrato, no en el código que lo consume. `providers` es la unión literal `"all" | [ids]` de la spec; "ausente significa todos" sería semántica implícita en el documento de mayor costo de cambio del proyecto. Un pack es composición: declara `includes[]` y no trae archivos propios, pero **sí declara** sus `providers` y `requiredCapabilities` en vez de derivarlos, para que una card del marketplace no tenga que calcular uniones — la coherencia con sus miembros la verifica el core. `description` es un mapa abierto ISO 639-1 con `es` y `en` obligatorios, alineado con `artifactLanguage`: un tercer idioma pasa a ser dato del catálogo, no cambio de contrato. **Sin versión por ítem**: las definiciones se versionan por el tag que las publica (`01:77`, T-061 cachea por versión), así que la referencia de un proyecto es este `id` unido al `platformVersion` del manifiesto; añadir un campo opcional después es aditivo y seguro, congelarlo hoy sin necesidad no.

**Dos esquemas de T-010 tocados, a propósito y por la convención de un solo sitio por concepto.** `platformVersion` de `project-config` pasa de patrón en línea a `$ref` —cambio puro de referencia, cero cambio semántico—. El identificador de proveedor se estrecha a `providerId` en `project-config.providers[].id` y en las claves de `personalization.providerSettings`: antes era cualquier cadena no vacía, y un typo ahí direccionaba silenciosamente a un proveedor inexistente. `catalogId` se extrae para que el manifiesto nombre un ítem sin versión, y **un test exige que siga siendo el prefijo literal de `catalogRef`**: si derivaran, el manifiesto podría publicar un ítem legal que ningún proyecto puede referenciar.

**Pruebas.** Fixtures válidas e inválidas como en T-010, con la advertencia heredada respetada: una causa que solo aparece en la cascada de `unevaluatedProperties` no localiza nada, así que las inválidas rompen reglas que producen causa sustantiva o cuya única causa es la del campo que nombran. Arnés verificado contra diez regresiones en una copia desechable —manifiesto con envolvente de artefacto, artefacto con envolvente de manifiesto, esquema con dos envolventes, `catalogId` ensanchado, patrón de `path` aflojado, las dos reglas condicionales de `role` y `files` aflojadas, ítem abierto, fixture con nombre mentiroso y esquema nuevo sin fixtures—: las diez fallan.

**Herencia para T-060/T-061.** El manifiesto declara y no deriva, así que T-061 arrastra cinco reglas que este esquema no puede expresar —unicidad del `id` de ítem, resolución de `includes[]`, cobertura de un pack sobre sus miembros, digest contra archivo descargado y unicidad del `path` dentro de un ítem—; T-050 arrastra una sexta, la pertenencia al catálogo de capacidades. Las seis están en la tabla del `README.md` de `schemas/`. La del `path` la destapó la revisión: `uniqueItems` solo rechaza entradas idénticas, así que dos con la misma ruta y distinto digest pasaban, dejando implícito cuál vale; y la misma comprobación tiene que ignorar la caja, porque dos rutas que solo difieren en mayúsculas colisionan al materializar en macOS o Windows. Es exactamente el hueco que T-010 ya había documentado para el `id` de etapa de un flujo, y se cierra igual: en el código, no fingiendo que el esquema puede.

**T-012 ∥ · Esquema de definición canónica de agente y de flujo declarativo** *(estado: **completada**)*
Formato agnóstico de agente (incluida la sección de personalización delimitada) y formato de flujo (etapas, participantes por rol, entradas, salidas con esquema, omitibilidad, checklist de datos bloqueantes). Se revisó en dos rondas —agente primero, flujo después— porque una sola aprobación sobre dos contratos públicos es peor revisión que dos.

**Cuarta familia de esquema.** Una definición canónica no es artefacto de proyecto ni es el manifiesto: la publica el repositorio de definiciones y, a diferencia del manifiesto, **no lleva versión propia** — la versión de una definición es el tag que la publica, y el manifiesto de ese tag es lo que une este `id` a una versión. De ahí `definitionEnvelope` (`schemaVersion`, `id` del catálogo). El endurecimiento de T-011 se extendió sin maquinaria nueva: dos entradas más en la tabla de familias.

**Instrucciones en markdown referenciado (decisión del usuario).** ADR-013 fija "formato propio JSON, compilado por cada adaptador"; se lee como que el *formato de definición* es JSON —frente a mantener N formatos nativos— y no como que cada byte viva dentro del JSON. Lo que decide no es ADR-013 sino T-052: la comparación tres vías y el `adoptar` de R7 operan sobre el archivo materializado, y un diff sobre una cadena JSON con saltos escapados no lo lee nadie. La definición nombra el archivo del cuerpo; el punto de inserción de la personalización es un `anchor` que el adaptador sustituye al compilar, y los delimitadores del archivo compilado son del dialecto nativo, no de este contrato.

**La definición declara, el manifiesto indexa.** T-060 genera el manifiesto desde las definiciones. Al construir el esquema de flujo se destapó que la dirección no se sostenía: `name` y `providers` del manifiesto **no eran derivables** de ninguna definición. Se añaden a ambas —`name` es el nombre del catálogo (`Architect`), distinto del `defaultDisplayName` de persona (`Andrés`)— y `providerScope` sube a `common`. No contradice lo decidido en T-011 para los packs, que sí declaran lo suyo: un pack no tiene archivo de definición, su entrada del manifiesto **es** su definición.

**El flujo, contra `flow-progress` congelado.** Tres acoplamientos verificados y no supuestos. `stageId` y `roleId` suben a `common` y los referencian los dos documentos. Esto **estrecha de verdad** un contrato que T-010 ya había congelado —de `{string, minLength 1}` a patrón kebab, así que un documento con `role: "Business Agent"` que antes validaba ahora no—; el riesgo práctico es nulo porque no hay tag publicado, y se comprueba que las fixtures congeladas ya lo cumplían, pero es un estrechamiento y no una mudanza. El tercero era real: `outputRefs` de una etapa cerrada son `reference`, cuyo enum no incluye la configuración, pero el mínimo bloqueante de la etapa 3 aterriza en `config.json`. Se resuelve sin ensanchar nada: la salida de esa etapa es el **documento** de análisis técnico y la escritura en configuración es efecto de la extracción, transportado por el checklist de datos bloqueantes que escribe T-071.

**Lecturas confirmadas por el usuario.** `inputs` "vía índice" describe *cómo el motor recupera* los documentos, no el selector: una etapa nombra etapas previas y, opcionalmente, etiquetas —que es lo que el índice de T-022 sabe consultar, y por donde entra el documento que aporta el usuario en T-073—. `blockingDataKey` lleva patrón y no enum, con la pertenencia como regla de código: mismo trade-off que `requiredCapabilities`. Fuera del contrato queda el moderador con agenda, que es de M2 y tiene fixture inválida que lo deja ejecutable; `participants: []` es la forma de decir que una etapa es determinística, que es lo que el scaffold es.

**Defecto encontrado y cerrado: `propertyNames` no localiza.** Escribiendo la fixture de una clave inválida el test falló, y el volcado del árbol de errores del validador mostró la causa: `propertyNames` reporta con la **ubicación de instancia vacía en todos los niveles**, así que el campo culpable no viaja en el error y el rechazo no puede decir qué reparar — exactamente lo que el README promete que no pasa. Cuatro sitios lo usaban (`localizedText` y el `providerSettings` de `agents.json`, ambos de T-011; el de esta tarea; y `schemaVersions` de T-010), y **ninguna fixture, desde T-010, ejercitaba jamás una clave inválida**: por eso llevaba tres tareas escondido. Los cuatro pasan a `patternProperties` + `additionalProperties: false`, que restringe lo mismo y falla en la ubicación del mapa; un test prohíbe `propertyNames` a cualquier profundidad y otro mantiene iguales los patrones que quedan duplicados por ser claves de regex, que no admiten `$ref`. Se añadieron las cuatro fixtures que faltaban.

**Otras extracciones a `common`.** `languageCode` (estaba en línea en tres sitios), `artifactType` (estaba en línea dentro de `reference`) y `definitionPath` — el patrón anti-traversal de T-011, que ahora existe **en una sola copia** en vez de duplicarse entre el manifiesto y la definición de agente: una regla de seguridad duplicada es una regla que se pudre.

**Pruebas.** La fixture válida principal del flujo es el **flujo de inicialización de M1 completo**, con sus cinco etapas y sus ids coincidiendo con los de las fixtures de `flow-progress`: la prueba de que el esquema expresa lo que la tarea existe para expresar. Arnés verificado contra seis regresiones en copia desechable —vuelta a `propertyNames`, `propertyNames` escondido dentro de un `allOf`, clave de patrón derivando de `common`, definición con envolvente de artefacto, patrón anti-traversal aflojado y `localizedText` dejando de exigir inglés—: las seis fallan.

**Lo que destapó la revisión.** `roleId` es el tercer vocabulario abierto por patrón de esta tarea, y era el único sin fila en la tabla de reglas de código: nada ata el rol que un progreso registra a los que su etapa declara. La fila faltaba y el hueco no era teórico —las fixtures congeladas de `flow-progress` registraban `business-agent` en la exploración, rol que no existe en el catálogo de `03-arquitectura.md:130-137`, y `product-owner` en la planificación, que contradice a `project-manager` y a la regla de un agente por etapa de M1—. Se añade la fila y se corrigen los tres valores, con lo que el corpus entero concuerda con el catálogo canónico. Además, el test que ata los patrones duplicados deja de ser una lista de sitios a mano y pasa a barrer el corpus entero: un mapa nuevo con clave de patrón queda cubierto solo, y una exención que sobreviva al mapa para el que se escribió también falla. La única exención viva es `schemaVersions`, cuyas claves son deliberadamente un espacio más ancho que el enum de `artifactType`.

**Herencia para T-050, T-060 y T-070.** Diez reglas nuevas que el esquema no puede expresar están en la tabla del `README.md` de `schemas/`, entre ellas que el `anchor` aparezca exactamente una vez en el cuerpo (T-052), que `inputs.stages` nombre etapas anteriores del mismo flujo (T-070) y que el manifiesto coincida con las definiciones de las que se genera (T-060).

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

**T-030 · Envoltura segura de git** *(estado: **completada**, depende de T-001)*
Detección de binario y versión mínima, invocación por arrays (sin shell), parseo `--porcelain`. Validación/solicitud de identidad (user.name/email) con bloqueo al primer uso.

Alcance estrictamente el paquete `gitops/`: detección + versión mínima, la primitiva de invocación segura, `git status --porcelain=v1` y la identidad. Worktrees/ramas (T-031), changelog (T-032) y todo lo de `store/`/`core/` quedan fuera, sin tocarlos.

**Versión mínima: 2.30.0, cota inferior únicamente.** ADR-004 deja la cifra a la implementación ("dependencia de versión mínima de git a declarar"). Se elige 2.30.0 (diciembre 2020) por ser lo bastante antigua para estar disponible en los repositorios por defecto de distribuciones Linux LTS aún en uso, y lo bastante moderna para que `--porcelain=v1 -z` y `git config --get` se comporten exactamente como documentado —ambos llevan estables mucho más tiempo que ese piso—. A diferencia de los adaptadores de proveedor (§7, que declaran un *rango* de versiones soportadas porque el proveedor es un objetivo móvil de terceros), git es un prerrequisito del sistema: solo cota inferior, sin techo. `MinimumVersionMajor/Minor/Patch` son las constantes literales que pide la tarea; `MinimumVersion` es el valor `Version` derivado para comparar directamente contra `Binary.Version`.

**`--porcelain=v1 -z`, no v1 a secas.** La tarea permitía v2 si se justificaba; se justifica quedarse en v1 pero añadiendo `-z`: sin `-z`, git aplica su citado estilo C a rutas "inusuales" (espacios, comillas, bytes no-ASCII), lo que habría obligado a reimplementar esas reglas de citado como un segundo lugar, más difícil de auditar, donde el "parseo total" podía fallar en silencio. Con `-z`, cada registro llega verbatim y terminado en NUL — verificado empíricamente contra git 2.50.1 con archivos con espacios, un rename y una eliminación antes de escribir el parser, no asumido de la documentación. Se añade además `--untracked-files=normal`: sin fijarlo, `status.showUntrackedFiles=no` en la configuración del usuario haría que `Status` omitiera archivos sin seguimiento en silencio — quien use `Status` para decidir si un worktree está limpio vería "limpio" sobre un árbol que no lo está, exactamente el hueco que el parser total existe para evitar. Verificado que las variantes de configuración (`status.branch`, `status.short`) no inyectan una cabecera `##` en el formato porcelain v1, así que no hace falta `--no-branch` defensivo.

**El parser es total: `patternProperties`-como para el estado, en vez de aceptar cualquier byte.** `parsePorcelainV1Z` exige el prefijo `"XY "` de tres bytes, valida X e Y contra el catálogo documentado (` MADRCUT`, más los pares `??`/`!!` que deben aparecer casados), y consume el campo de origen adicional para rename/copy — cualquier desviación es un error explícito, no una línea descartada. `T` (typechange, p. ej. un archivo reemplazado por un symlink) se incluyó tras una revisión: es un estado normal del repositorio, no un caso límite, y omitirlo habría convertido una llamada rutinaria a `Status` en un fallo — verificado con un symlink real antes de escribir la fixture. El caso "línea malformada" pedido por la tarea se cubre en dos frentes: un test de función pura contra secuencias de bytes sintéticas (siete variantes) y la integración contra un repo real con seis estados simultáneos (altas en stage, modificación con y sin stage, borrado sin stage, rename en stage, sin seguimiento).

**Identidad: precedencia completa, no solo `--local`.** `Identity` lee `git config --get user.name/user.email` sin forzar `--local`, es decir la misma cadena local→global→sistema que usaría `git commit`. Coincide con la lectura de la spec ("identidad = configuración git del proyecto"): el proyecto puede heredar una identidad global del desarrollador, y eso es válido. Un valor presente pero vacío (`git config user.name ""`, que sale con código 0) se trata igual que ausente — ninguno de los dos es una atribución utilizable. El mensaje de bloqueo nombra el comando exacto (`git config user.name/email "..."`) y aclara que sin `--global` queda scopeado a este repositorio, con la alternativa `--global` mencionada explícitamente para quien quiera identidad de máquina.

**Superficie pública deliberadamente angosta.** `Binary`, `Locate`/`LocateNamed`, `Version`, `StatusEntry`, `Identity` y los errores centinela (`ErrGitNotFound`, `ErrVersionTooOld`, `ErrVersionUnreadable`, `ErrIdentityIncomplete`) son lo único exportado. La primitiva de invocación (`runRaw`/`run`, `runError`) queda sin exportar a propósito: ningún llamador externo al paquete puede hoy construir una invocación git arbitraria, ni por accidente ni por descuido — la superficie pública no tiene ningún punto donde un valor externo se convierta en un argumento de git. Es la lectura más fuerte posible de "diseña la API de forma que construir un comando inseguro sea difícil": no difícil, imposible desde fuera del paquete en el estado actual. T-031 y T-032 viven en el mismo paquete (`gitops/doc.go` ya declara worktrees/ramas y commit/push gobernado como sus responsabilidades), así que ampliarán esta primitiva *dentro* del paquete — evolución esperada, no una excepción a nada decidido aquí.

**Hueco documentado, no cerrado: herencia de entorno.** `runRaw` deja `cmd.Env` sin fijar, por lo que hereda el entorno del proceso llamante tal cual — es lo que permite a los tests aislar la resolución de configuración de git con `t.Setenv` (`GIT_CONFIG_NOSYSTEM`, `GIT_CONFIG_GLOBAL`), pero también significa que unas `GIT_DIR`/`GIT_WORK_TREE`/`GIT_INDEX_FILE` ambientales podrían redirigir una invocación fuera de `dir`. Queda anotado en el código para quien primero necesite una garantía de directorio más fuerte que "sin shell interpolado" — T-031 es el dueño natural de esa decisión.

**Fake git compilado, no un script.** El helper de pruebas (`gitops/testdata/fakegit`) es un binario Go de ~60 líneas controlado por variables de entorno (versión simulada, volcado de argv, código de salida), compilado en `TestMain` antes de cualquier test. Se descartó un script de shell: no habría corrido igual en Windows, donde `exec.LookPath`/`exec.Command` no interpretan `#!/bin/sh`, y la matriz de CI incluye `windows/amd64`. `testdata/` es la ubicación establecida por el proyecto para código de soporte que `go build`/`go vet`/`go list ./...` deben ignorar pero que golangci-lint sí recorre (confirmado empíricamente: los `nolint` de `fakegit/main.go` se reportarían como no usados si el linter no lo escaneara, y no lo hicieron).

**Aislamiento de configuración en los tests de identidad y estado, real en todo el rango soportado.** La primera versión aislaba solo con `GIT_CONFIG_NOSYSTEM=1` + `GIT_CONFIG_GLOBAL` apuntando a una ruta inexistente — revisión detectó que `GIT_CONFIG_GLOBAL` existe recién desde git **2.32.0**, por debajo de `MinimumVersion` (2.30.0): en un git 2.30/2.31, que `Locate` acepta sin más, esa variable se ignora y la configuración de la máquina se cuela sin aviso. Se añade `HOME`/`USERPROFILE`/`XDG_CONFIG_HOME` apuntando a un directorio vacío de `t.TempDir()`, que es el mecanismo por el que git descubre `$HOME/.gitconfig` o `$XDG_CONFIG_HOME/git/config` desde mucho antes del piso de versión de este paquete — verificado con un repo real y `GIT_CONFIG_GLOBAL`/`GIT_CONFIG_NOSYSTEM` explícitamente ausentes, que el aislamiento por `HOME` basta por sí solo. Los dos mecanismos quedan superpuestos a propósito, no uno sustituyendo al otro. Esto aísla cada repo temporal de lo que tenga configurado la máquina donde corren los tests, incluido el `core.autocrlf=true` que los runners de Windows de GitHub Actions traen en la configuración de sistema. Se aplica también a los tests de `Status`, no solo a los de `Identity`: un `autocrlf` o `showUntrackedFiles` ambiental podría alterar el conteo exacto de entradas que esos tests verifican, en un solo runner de la matriz. `realGit` (helper de test) falla cerrado (`t.Fatalf`) si no encuentra un git real, en vez de `t.Skip` — la misma postura que `internal/archtest` documenta para su propia dependencia de `go list`: un `Locate` roto no debe traducirse en que toda la suite de `Status`/`Identity` reporte verde sin haber probado nada.

**`IsIgnored()` no se añadió.** `Status` nunca pasa `--ignored`, así que el par `!!` no puede aparecer en su salida real; un método que solo podría devolver `false` es superficie pública inerte, y la virtud declarada de este paquete es una API deliberadamente angosta. El **parser** sí reconoce `!!` como par válido — su totalidad frente a cualquier entrada es una propiedad distinta y deseable, independiente de qué banderas pase `Status` hoy.

**Herencia para T-031: disciplina de `--` y de pathspec.** Hoy la inyección de argumentos es inalcanzable por diseño: ninguna función pública de este paquete acepta un valor del llamador que termine como elemento de `args` — `Status` e `Identity` solo construyen subcomandos con literales fijos. Eso deja de ser cierto en cuanto T-031 empiece a pasar nombres de rama y rutas influidos por el usuario hacia el primitivo interno (`run`/`runRaw`): el array de argumentos impide la inyección de *shell*, pero no protege por sí solo contra un valor que empiece por `-` y que git interprete como una opción en vez de como un nombre de rama o una ruta. T-031 tiene que decidir, en el punto donde el primer valor externo entre a `args ...string`, entre separador `--` (para el caso general) o el prefijo `./` en rutas (el idiom de pathspec de git) — no es una elección que este paquete resuelva por adelantado sin un caso de uso concreto delante.

**Ambigüedad reportada, no resuelta por adivinanza.** La tarea menciona "fuera de rango" al hablar de la versión mínima; se interpretó como "por debajo del mínimo" (sin techo), documentado arriba — no hay otra lectura consistente con que ADR-004 solo pida declarar un mínimo.

Ficheros: `gitops/{version,binary,run,status,identity}.go` y sus `_test.go`, más `gitops/testdata/fakegit/main.go`. `make verify` (fmt-check + vet + lint + `-race`) y `make build-all` pasan limpios.

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
