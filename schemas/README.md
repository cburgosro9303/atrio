# Esquemas de artefactos de Atrio

Contrato público de Atrio (ADR-005). Doce archivos: una definición común, los ocho artefactos de gestión que congeló T-010, el manifiesto del marketplace que congeló T-011 y las dos definiciones canónicas del catálogo que congeló T-012.

| Archivo | Artefacto | Dónde vive |
|---|---|---|
| `common.schema.json` | Envolventes y tipos compartidos | — (solo `$defs`) |
| `task.schema.json` | Tarea y bug | `.atrio/management/tasks/{ulid}.json` |
| `decision.schema.json` | Decisión | `.atrio/management/decisions/{ulid}.json` |
| `log-entry.schema.json` | Entrada de bitácora | `.atrio/management/log/{ulid}.json` |
| `changelog.schema.json` | Changelog | `.atrio/management/changelogs/{ulid}.json` |
| `flow-progress.schema.json` | Progreso de flujo | `.atrio/management/flows/{ulid}.json` |
| `project-config.schema.json` | Configuración de proyecto | `.atrio/config.json` |
| `agents.schema.json` | Declaración, personalización y permisos | `.atrio/agents.json` |
| `document-front-matter.schema.json` | Front-matter documental (forma ya parseada) | cabecera YAML de cada `docs/**/*.md` |
| `marketplace-manifest.schema.json` | Manifiesto del marketplace | repositorio oficial de definiciones, uno por tag |
| `agent-definition.schema.json` | Definición canónica de agente | repositorio oficial de definiciones |
| `flow-definition.schema.json` | Definición canónica de flujo declarativo | repositorio oficial de definiciones |

## Convenciones

- **Draft 2020-12.** No es una preferencia: la envolvente se compone con `allOf` y cada artefacto se cierra con `unevaluatedProperties: false`, que es la combinación que draft-07 no permite.
- **`$id` relativo**, idéntico al nombre del archivo, y `$ref` relativos entre esquemas. Atrio todavía no tiene dominio propio (ADR-015 deja abierta la organización), y un `$id` absoluto sería una URL que el proyecto no controla. Publicarlos bajo una base (T-082) no exige tocar los archivos.
- **`schemaVersion` entero** y monótono por tipo de artefacto: la versión `N` lee `N-1` y `N-2` y siempre escribe `N` (`03-arquitectura.md`, §12). `config.json` lleva además el mapa `schemaVersions` para migraciones selectivas.
- **Identificadores y valores de enum en inglés.** El contrato es código; el idioma del proyecto (`artifactLanguage`) gobierna lo que un equipo escribe, y traducir etiquetas es cosa de presentación.
- **Ningún campo desconocido pasa.** Un campo no declarado es un typo en un campo que sí importa. La única puerta abierta a propósito es `personalization.providerSettings` de `agents.json`: la forma de los ajustes específicos de cada proveedor la conoce su adaptador, no este contrato.
- **Un solo sitio por concepto.** El `id@version` del caché y su mitad de identificador, las siete categorías de permisos, el patrón de rama, la versión de plataforma y el identificador de proveedor viven en `common.schema.json` y los demás esquemas los referencian. Dos pares están sincronizados por un test y no por un comentario: las categorías, que aparecen como claves del mapa en `agents.json` y como valor de enum en la bitácora; y `catalogId`, que tiene que seguir siendo el prefijo literal de `catalogRef` — si derivaran, el manifiesto podría publicar un ítem perfectamente legal que ningún proyecto puede referenciar.
- **Cuatro envolventes, y cada esquema lleva exactamente la suya.** Los artefactos JSON llevan la completa (`schemaVersion`, `id`, `createdAt`, `updatedAt`, `createdBy`); el front-matter lleva la reducida (`schemaVersion`, `id`), porque un markdown se edita a mano y las fechas y la autoría ya las sabe git; el manifiesto lleva la suya (`manifestVersion`, `platformVersion`), porque no es un artefacto de un proyecto sino un índice que el repositorio de definiciones publica por tag; y una definición canónica lleva `definitionEnvelope` (`schemaVersion`, `id` del catálogo), que además **no lleva versión propia**: la versión de una definición es el tag que la publica. El test no se conforma con que haya *una* envolvente: exige la de la familia del esquema, así que un manifiesto que se declare artefacto falla igual que un artefacto que se declare manifiesto.
- **Nunca `propertyNames`.** Restringir las claves de un mapa con esa palabra clave es lo primero que uno alcanza y rompe en silencio la promesa de esta suite: el validador reporta un fallo de `propertyNames` con la **ubicación de instancia vacía**, así que el rechazo no puede decir en qué campo estaba la clave mala. Se usa `patternProperties` con `additionalProperties: false`, que restringe exactamente lo mismo y falla en la ubicación del mapa. Un test lo prohíbe a cualquier profundidad. Coste asumido: la clave de `patternProperties` es una expresión regular literal y no admite `$ref`, así que los patrones que espejan una definición de `common` quedan duplicados; otro test los mantiene iguales.
- **`format` se afirma por consumidor, no por el contrato.** `common.schema.json` declara `"format": "date-time"` en `timestamp` y `"format": "email"` en `actor.email`, ninguno de los dos con `pattern` de respaldo — pero la aserción de `format` está apagada por defecto en la librería de validación para draft 2019-09 en adelante, y el arnés de pruebas de este paquete (`schemas/schemas_test.go`) compila con esa configuración por defecto. Eso significa que **esta suite no comprueba en absoluto** si `createdAt`/`updatedAt` son una fecha real o si `createdBy.email` es un email real: solo confirma que son cadenas. Activar la aserción es una decisión de cada consumidor que compila estos esquemas por su cuenta. Hoy el único que lo hace es `store/` (`store/validate.go`, `compiler.AssertFormat()`), verificado contra el corpus de fixtures válidas de este paquete antes de apoyarse en la decisión (`store/frozen_fixtures_test.go`) para confirmar que activar `format` no estrecha lo que T-010/T-011/T-012 ya congelaron como válido. Un consumidor futuro que compile estos esquemas (`flows/`, `api/`) y no active `AssertFormat()` heredará el mismo hueco.

## Qué valida el esquema y qué valida el código

Un esquema ve un documento aislado. Las reglas que comparan versiones o cruzan artefactos no caben ahí, y fingir que sí es peor que declararlo:

| Regla | Dónde |
|---|---|
| `blockedBy` obligatorio en una tarea bloqueada | esquema (`task`) |
| El bucle por ambiente nombra su ambiente | esquema (`task`) |
| Solo un bug nace en `triage` y solo un bug viene de otra tarea | esquema (`task`) |
| Un despliegue lo confirma una persona, nunca un agente | esquema (`task`) |
| Una decisión reemplazada dice por cuál | esquema (`decision`) |
| Una autorización registra qué, a qué agente y quién la concedió | esquema (`log-entry`) |
| Un renombrado dice de dónde viene | esquema (`changelog`) |
| Una etapa cerrada persiste su artefacto; una omitida, su motivo | esquema (`flow-progress`) |
| Un bloque de permisos declara perfil o mapa, y el mapa decide las 7 categorías | esquema (`agents`) |
| Un agente del catálogo nombra su rol, y solo un agente lo tiene | esquema (`marketplace-manifest`) |
| Un pack es composición: nombra lo que agrupa y no trae archivos propios | esquema (`marketplace-manifest`) |
| Un archivo del catálogo no puede escapar de su carpeta (`.`/`..` irrepresentables) | esquema (`marketplace-manifest`) |
| Unicidad del `id` de ítem dentro de un manifiesto | código (T-061) — es regla entre hermanos del mismo documento |
| Unicidad del `path` dentro de un ítem, sin distinguir mayúsculas | código (T-061) — `uniqueItems` no sirve: dos entradas con el mismo `path` y distinto `sha256` lo pasarían, y la ruta que se materializa colisiona igual en un filesystem que ignora la caja |
| Los `includes[]` de un pack apuntan a ítems del mismo manifiesto | código (T-061) |
| Los `providers` y `requiredCapabilities` de un pack cubren los de sus miembros | código (T-061) — el manifiesto los declara, no los deriva, para que una card no tenga que calcular uniones |
| `requiredCapabilities` pertenece al catálogo cerrado de capacidades | código (T-050) — el catálogo lo cierra el adaptador, no este contrato |
| El `anchor` de personalización aparece exactamente una vez en el cuerpo de instrucciones | código (T-052) — cruza el JSON y el markdown que nombra |
| El archivo que `instructions` nombra existe dentro de la definición | código (T-060) |
| Unicidad del `id` de etapa dentro de una definición de flujo | código (T-070) — `uniqueItems` no sirve: dos etapas con el mismo `id` y distinto contenido lo pasarían |
| `inputs.stages` nombra etapas del mismo flujo, y anteriores a la que las declara | código (T-070) |
| `resolves` nombra claves declaradas en `blockingData` del mismo flujo | código (T-072) |
| Los `participants` de una etapa tienen agente declarado en el proyecto | código (T-070) — cruza la definición con `agents.json` |
| `blockingData[].key` pertenece al conjunto que el core sabe accionar | código (T-072/T-074) |
| El `role` que registra un progreso es uno de los que su etapa declara | código (T-070/T-071) — `roleId` es la tercera vocabulario abierto por patrón, y cruza cuatro documentos |
| El manifiesto coincide con las definiciones de las que se genera | código (T-060) — la definición declara, el manifiesto indexa |
| El `sha256` declarado casa con el archivo descargado | código (T-061) |
| Estados definitivos (`completed`, `cancelled`) inmutables | código — compara la versión anterior con la nueva (T-040) |
| Decisión inmutable salvo la transición a `superseded` | código (T-040) |
| `artifactLanguage` inmutable tras la creación | código (T-020) |
| Unicidad de `displayName` entre agentes | código (T-020/T-041) |
| Unicidad del `id` de etapa dentro de un progreso de flujo | código (T-070) — `uniqueItems` no sirve: dos etapas con el mismo `id` y distinto contenido lo pasarían |
| Bitácora append-only | código — es regla del almacén, no del documento (T-020) |
| `closureEnvironment` ∈ `environments` del proyecto | código — cruza dos artefactos (T-040) |
| Los `refs`/`target` apuntan a artefactos que existen | código (T-020, T-022) |
| Orden legal de transiciones de estado | código (T-040, T-070) |
| Expansión perfil → mapa de permisos | código (T-041) |

## Pruebas

`go test ./schemas/` compila cada esquema contra el meta-esquema, comprueba las convenciones de arriba y ejerce cada uno contra sus fixtures de `testdata/`. Una fixture inválida se llama `<propiedad>--<motivo>.json`, y el test no se conforma con que sea rechazada: exige que el error del validador **apunte a esa propiedad**, recorriendo el árbol de causas. Un rechazo que no sabe decir qué campo está mal no es el error reparador que la plataforma promete.
