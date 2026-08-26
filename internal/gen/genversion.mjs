// Generates a protocol/version_<v>.go table from minecraft-data.
//
// Usage: node genversion.mjs <minecraft-data-dir> <version> <out.go>
//   e.g. node genversion.mjs /tmp/package/minecraft-data/data 26.1 ../../protocol/version_26_1.go
//
// Everything version-specific lives in the generated file: packet IDs, entity
// and item names, block-state classification and the chunk framing flags.
// Adding a version should mean running this, not editing hand-written code.
//
// The generator is deliberately loud. A packet this client needs that has been
// renamed or removed upstream is reported by name, because the alternative is
// a table with a silently wrong ID — and a wrong ID does not error, it decodes
// a different packet's fields and desynchronises everything after it.
import fs from 'node:fs'
import path from 'node:path'

const [, , dataDir, version, outPath] = process.argv
if (!dataDir || !version || !outPath) {
  console.error('usage: genversion.mjs <minecraft-data-dir> <version> <out.go>')
  process.exit(1)
}

const dir = path.join(dataDir, 'pc', version)
const read = f => JSON.parse(fs.readFileSync(path.join(dir, f), 'utf8'))
const protocol = read('protocol.json')
const entities = read('entities.json')
const blocks = read('blocks.json')
const items = read('items.json')
const versionInfo = read('version.json')

// --- packet IDs ------------------------------------------------------------
// Go field name -> [state, direction, minecraft-data packet name].
//
// This list *is* the client's packet surface: if it is not here, the client
// neither sends nor decodes it. 26.1 has 141 clientbound play packets and this
// names two dozen.
const PACKETS = [
  ['SBHandshake', 'handshaking', 'toServer', 'set_protocol'],

  ['SBLoginStart', 'login', 'toServer', 'login_start'],
  ['SBLoginAcknowledged', 'login', 'toServer', 'login_acknowledged'],
  ['CBLoginDisconnect', 'login', 'toClient', 'disconnect'],
  ['CBLoginEncryptionBegin', 'login', 'toClient', 'encryption_begin'],
  ['CBLoginSuccess', 'login', 'toClient', 'success'],
  ['CBLoginCompress', 'login', 'toClient', 'compress'],

  ['SBConfigSettings', 'configuration', 'toServer', 'settings'],
  ['SBConfigFinishConfiguration', 'configuration', 'toServer', 'finish_configuration'],
  ['SBConfigKeepAlive', 'configuration', 'toServer', 'keep_alive'],
  ['SBConfigPong', 'configuration', 'toServer', 'pong'],
  ['SBConfigSelectKnownPacks', 'configuration', 'toServer', 'select_known_packs'],
  ['SBConfigAcceptCodeOfConduct', 'configuration', 'toServer', 'accept_code_of_conduct'],
  ['CBConfigDisconnect', 'configuration', 'toClient', 'disconnect'],
  ['CBConfigFinishConfiguration', 'configuration', 'toClient', 'finish_configuration'],
  ['CBConfigKeepAlive', 'configuration', 'toClient', 'keep_alive'],
  ['CBConfigPing', 'configuration', 'toClient', 'ping'],
  ['CBConfigSelectKnownPacks', 'configuration', 'toClient', 'select_known_packs'],
  ['CBConfigCodeOfConduct', 'configuration', 'toClient', 'code_of_conduct'],

  ['SBPlayTeleportConfirm', 'play', 'toServer', 'teleport_confirm'],
  ['SBPlayAttack', 'play', 'toServer', 'attack'],
  ['SBPlayChatMessage', 'play', 'toServer', 'chat_message'],
  ['SBPlayClientCommand', 'play', 'toServer', 'client_command'],
  ['SBPlayUseEntity', 'play', 'toServer', 'use_entity'],
  ['SBPlayUseItem', 'play', 'toServer', 'use_item'],
  ['SBPlayKeepAlive', 'play', 'toServer', 'keep_alive'],
  ['SBPlayPosition', 'play', 'toServer', 'position'],
  ['SBPlayPositionLook', 'play', 'toServer', 'position_look'],
  ['SBPlayLook', 'play', 'toServer', 'look'],
  ['SBPlayBlockDig', 'play', 'toServer', 'block_dig'],
  ['SBPlayHeldItemSlot', 'play', 'toServer', 'held_item_slot'],
  ['SBPlayArmAnimation', 'play', 'toServer', 'arm_animation'],
  ['SBPlayBlockPlace', 'play', 'toServer', 'block_place'],
  ['SBPlayWindowClick', 'play', 'toServer', 'window_click'],
  ['SBPlayCloseWindow', 'play', 'toServer', 'close_window'],
  ['SBPlaySetCreativeSlot', 'play', 'toServer', 'set_creative_slot'],
  ['SBPlayPlayerInput', 'play', 'toServer', 'player_input'],
  ['SBPlayEntityAction', 'play', 'toServer', 'entity_action'],
  ['SBPlayChunkBatchReceived', 'play', 'toServer', 'chunk_batch_received'],
  ['SBPlayPlayerLoaded', 'play', 'toServer', 'player_loaded'],

  ['CBPlaySpawnEntity', 'play', 'toClient', 'spawn_entity'],
  ['CBPlayBlockChange', 'play', 'toClient', 'block_change'],
  ['CBPlayKickDisconnect', 'play', 'toClient', 'kick_disconnect'],
  ['CBPlayUnloadChunk', 'play', 'toClient', 'unload_chunk'],
  ['CBPlayKeepAlive', 'play', 'toClient', 'keep_alive'],
  ['CBPlayMapChunk', 'play', 'toClient', 'map_chunk'],
  ['CBPlayLogin', 'play', 'toClient', 'login'],
  ['CBPlayRelEntityMove', 'play', 'toClient', 'rel_entity_move'],
  ['CBPlayEntityMoveLook', 'play', 'toClient', 'entity_move_look'],
  ['CBPlayEntityTeleport', 'play', 'toClient', 'entity_teleport'],
  ['CBPlayDeathCombatEvent', 'play', 'toClient', 'death_combat_event'],
  ['CBPlayPosition', 'play', 'toClient', 'position'],
  ['CBPlayEntityDestroy', 'play', 'toClient', 'entity_destroy'],
  ['CBPlayMultiBlockChange', 'play', 'toClient', 'multi_block_change'],
  ['CBPlayRespawn', 'play', 'toClient', 'respawn'],
  ['CBPlayUpdateHealth', 'play', 'toClient', 'update_health'],
  ['CBPlayWindowItems', 'play', 'toClient', 'window_items'],
  ['CBPlaySetSlot', 'play', 'toClient', 'set_slot'],
  ['CBPlayOpenWindow', 'play', 'toClient', 'open_window'],
  ['CBPlayHeldItemSlot', 'play', 'toClient', 'held_item_slot'],
  ['CBPlayCollect', 'play', 'toClient', 'collect'],
  ['CBPlayChunkBatchStart', 'play', 'toClient', 'chunk_batch_start'],
  ['CBPlayChunkBatchFinished', 'play', 'toClient', 'chunk_batch_finished'],
]

// Packets this client can live without. Everything else missing is fatal:
// silently emitting -1 for a packet the client actually sends would turn a
// send into a no-op with no error anywhere.
const OPTIONAL = new Set([
  'SBPlayAttack',               // 26.1+; before that, attacking rode use_entity
  'SBPlayPlayerLoaded',         // 1.21.4+
  'SBConfigAcceptCodeOfConduct', // 26.1+
  'CBConfigCodeOfConduct',       // 26.1+
  'SBPlayChunkBatchReceived',
  'CBPlayChunkBatchStart',
  'CBPlayChunkBatchFinished',
])

// packetIDs builds name -> id for one state and direction.
function packetIDs(state, direction) {
  const container = protocol[state]?.[direction]?.types?.packet
  if (!container) return {}
  const nameField = container[1].find(f => f.name === 'name')
  const mappings = nameField.type[1].mappings
  const out = {}
  for (const [hexID, name] of Object.entries(mappings)) out[name] = parseInt(hexID, 16)
  return out
}

const idCache = new Map()
function lookupPacket(state, direction, name) {
  const key = `${state}/${direction}`
  if (!idCache.has(key)) idCache.set(key, packetIDs(state, direction))
  const id = idCache.get(key)[name]
  return id === undefined ? null : id
}

const packetFields = []
const missing = []
for (const [field, state, direction, name] of PACKETS) {
  const id = lookupPacket(state, direction, name)
  if (id === null) {
    if (!OPTIONAL.has(field)) missing.push(`${field} (${state}/${direction}/${name})`)
    packetFields.push([field, -1])
    continue
  }
  packetFields.push([field, id])
}
if (missing.length) {
  console.error(`genversion: ${version} is missing required packets:\n  ` + missing.join('\n  '))
  process.exit(1)
}

// --- chunk format ----------------------------------------------------------
// The two format differences that cannot be expressed as a table. Both are
// invisible until wrong, and then surface as a short read several sections
// downstream — so they are stated per version rather than guessed.
//
//   HasSizePrefix: before 1.21.5 each paletted container carried a VarInt
//                  count of longs; from 1.21.5 it is computed.
//   HasFluidCount: from 26.1 each section carries a second int16 after the
//                  solid block count.
// Both thresholds are protocol numbers, taken from prismarine-chunk:
//   < 770 is pre-1.21.5, when the data array still carried its length
//   >= 775 is 26.1+, which added the per-section fluid count
const chunkFormat = {
  hasSizePrefix: versionInfo.version < 770,
  hasFluidCount: versionInfo.version >= 775,
}

// --- name tables -----------------------------------------------------------
// Indexed by wire ID, so gaps are empty strings rather than shifted entries.
function byID(list, valueOf) {
  const out = []
  for (const entry of list) {
    if (typeof entry.id !== 'number') continue
    out[entry.id] = valueOf(entry)
  }
  for (let i = 0; i < out.length; i++) if (out[i] === undefined) out[i] = ''
  return out
}

const entityNames = byID(entities, e => `minecraft:${e.name}`)
const itemNames = byID(items, i => `minecraft:${i.name}`)
const itemStacks = byID(items, i => i.stackSize ?? 64).map(v => (v === '' ? 0 : v))

// --- block state classification -------------------------------------------
// Emitted as sorted, merged [lo, hi] ranges: a few hundred entries instead of
// tens of thousands, cheap enough to binary-search for every block a downward
// scan touches.
const WATER = new Set(['water', 'bubble_column'])
const LAVA = new Set(['lava'])
const AIR = new Set(['air', 'cave_air', 'void_air'])

function ranges(predicate) {
  const spans = []
  for (const b of blocks) {
    if (!predicate(b)) continue
    const lo = b.minStateId ?? b.defaultState
    const hi = b.maxStateId ?? b.defaultState
    if (typeof lo !== 'number' || typeof hi !== 'number') continue
    spans.push([lo, hi])
  }
  spans.sort((a, b) => a[0] - b[0])

  const merged = []
  for (const span of spans) {
    const last = merged[merged.length - 1]
    if (last && span[0] <= last[1] + 1) {
      last[1] = Math.max(last[1], span[1])
    } else {
      merged.push([...span])
    }
  }
  return merged
}

// Solid means "blocks movement", which is the collision shape — deliberately
// not the same question as "would the crosshair stop here". See IsTargetable.
const solid = ranges(b => b.boundingBox === 'block' && !WATER.has(b.name) && !LAVA.has(b.name))
const water = ranges(b => WATER.has(b.name))
const lava = ranges(b => LAVA.has(b.name))
const air = ranges(b => AIR.has(b.name))

// --- emit ------------------------------------------------------------------
const goVar = 'v' + version.replace(/\./g, '_')
const q = s => JSON.stringify(s)

const lines = []
lines.push(`package protocol`)
lines.push(``)
lines.push(`// Minecraft ${versionInfo.minecraftVersion} (protocol ${versionInfo.version}).`)
lines.push(`//`)
lines.push(`// Code generated by internal/gen/genversion.mjs from minecraft-data. DO NOT EDIT.`)
lines.push(``)
lines.push(`func init() { Register(${goVar}) }`)
lines.push(``)
lines.push(`var ${goVar} = &Version{`)
lines.push(`\tName:     ${q(versionInfo.minecraftVersion)},`)
lines.push(`\tProtocol: ${versionInfo.version},`)
lines.push(`\tChunk: ChunkFormat{`)
lines.push(`\t\tHasFluidCount: ${chunkFormat.hasFluidCount},`)
lines.push(`\t\tHasSizePrefix: ${chunkFormat.hasSizePrefix},`)
lines.push(`\t},`)
lines.push(`\tPackets: PacketIDs{`)
for (const [field, id] of packetFields) lines.push(`\t\t${field}: ${id},`)
lines.push(`\t},`)
lines.push(`\tentityNames: ${goVar}EntityNames,`)
lines.push(`\titemNames:   ${goVar}ItemNames,`)
lines.push(`\titemStacks:  ${goVar}ItemStacks,`)
lines.push(`\tsolidStates: ${goVar}Solid,`)
lines.push(`\twaterStates: ${goVar}Water,`)
lines.push(`\tlavaStates:  ${goVar}Lava,`)
lines.push(`\tairStates:   ${goVar}Air,`)
lines.push(`}`)
lines.push(``)

function emitStrings(name, values) {
  lines.push(`var ${name} = []string{`)
  for (const v of values) lines.push(`\t${q(v)},`)
  lines.push(`}`)
  lines.push(``)
}

function emitInts(name, values) {
  lines.push(`var ${name} = []int32{`)
  for (const v of values) lines.push(`\t${v || 0},`)
  lines.push(`}`)
  lines.push(``)
}

function emitRanges(name, spans) {
  lines.push(`var ${name} = [][2]int32{`)
  for (const [lo, hi] of spans) lines.push(`\t{${lo}, ${hi}},`)
  lines.push(`}`)
  lines.push(``)
}

emitStrings(`${goVar}EntityNames`, entityNames)
emitStrings(`${goVar}ItemNames`, itemNames)
emitInts(`${goVar}ItemStacks`, itemStacks)
emitRanges(`${goVar}Solid`, solid)
emitRanges(`${goVar}Water`, water)
emitRanges(`${goVar}Lava`, lava)
emitRanges(`${goVar}Air`, air)

fs.writeFileSync(outPath, lines.join('\n'))
console.error(
  `genversion: ${version} -> ${outPath}: ` +
    `${packetFields.filter(([, id]) => id >= 0).length}/${PACKETS.length} packets, ` +
    `${entityNames.length} entities, ${itemNames.length} items, ` +
    `${solid.length} solid / ${water.length} water / ${lava.length} lava / ${air.length} air ranges`
)
