// Builds a minecraft-data-shaped directory for a version minecraft-data does
// not have yet, from the server's own generated reports.
//
// Usage: node reports-to-mcdata.mjs <reports-dir> <version> <protocol> <mcdata-dir> <borrow-version>
//
// # Why
//
// genversion.mjs is proven and should not be rewritten per version. But
// minecraft-data lags: 26.2 was released and is in neither the npm package nor
// the repository. Everything genversion needs is available from the server
// itself except two things, so this synthesises the input rather than forking
// the generator.
//
// Authoritative, taken from the reports:
//
//	item, entity and block ids     registries.json — and they move. Between
//	                               26.1 and 26.2, 1480 of 1506 item ids did.
//	block states                   blocks.json, every state with its id
//	max stack sizes                the per-item component report
//
// Borrowed from an earlier version, because the server does not report it:
//
//	collision shapes and bounding boxes    keyed by block name, so they survive
//	                                       the ids moving underneath them
//
// The borrow is the part to be suspicious of, so it is loud: any block present
// in the new version and absent from the borrowed data is listed, and any whose
// state count changed is listed separately, because a shape array indexed by
// state is meaningless if the states are not the same states.
import fs from 'node:fs'
import path from 'node:path'

// Shapes measured against a running server, for blocks whose kind is new and
// therefore has no precedent to borrow from.
//
// Measured by dropping an armour stand onto the block and reading where it
// comes to rest, with stone and a torch as controls so the method is checked
// before it is trusted. potent_sulfur rests a stand at the block top, so it is
// a full cube. sulfur_spike rests one at 0.6875 — eleven sixteenths, an exact
// multiple of 1/32, so nothing is being rounded.
//
// The height is measured; the horizontal extent is not, and is taken as full
// width. That is the conservative direction: a bot that thinks a block is wider
// than it is will path around it, where one that thinks it is narrower walks
// into it and desynchronises.
const MEASURED = {
  potent_sulfur: [[0, 0, 0, 1, 1, 1]],
  sulfur_spike: [[0, 0, 0, 1, 0.6875, 1]],
}

const [, , reportsDir, version, protocolStr, mcdataDir, borrow] = process.argv
if (!reportsDir || !version || !protocolStr || !mcdataDir || !borrow) {
  console.error('usage: reports-to-mcdata.mjs <reports-dir> <version> <protocol> <mcdata-dir> <borrow-version>')
  process.exit(1)
}
const protocol = Number(protocolStr)

const reports = p => JSON.parse(fs.readFileSync(path.join(reportsDir, p), 'utf8'))
const borrowed = p => JSON.parse(fs.readFileSync(path.join(mcdataDir, 'pc', borrow, p), 'utf8'))

const registries = reports('registries.json')
const blockReport = reports('blocks.json')
const bare = n => n.replace(/^minecraft:/, '')
const idsOf = reg =>
  Object.entries(registries[reg].entries).map(([name, e]) => ({ id: e.protocol_id, name: bare(name) }))

// Stack sizes come from the item component report, which lists every item's
// defaults. Anything without an explicit max_stack_size is the vanilla 64.
const componentDir = path.join(reportsDir, 'minecraft', 'components', 'item')
const stackOf = new Map()
if (fs.existsSync(componentDir)) {
  for (const f of fs.readdirSync(componentDir)) {
    if (!f.endsWith('.json')) continue
    const c = JSON.parse(fs.readFileSync(path.join(componentDir, f), 'utf8')).components ?? {}
    stackOf.set(f.slice(0, -5), c['minecraft:max_stack_size'] ?? 64)
  }
}

const entities = idsOf('minecraft:entity_type')
const items = idsOf('minecraft:item').map(i => ({ ...i, stackSize: stackOf.get(i.name) ?? 64 }))

// Blocks: ids and states from the report, shape-related fields borrowed.
//
// A block the earlier version never had can still be resolved, because the
// report says what *kind* of block it is and a kind pins the geometry. Checked
// rather than assumed: across every block in the borrowed version, each
// definition type — block, slab, stair, wall, rotated_pillar — resolves to
// exactly one collision geometry. Comparing the shape *ids* suggests otherwise
// and is a trap: two walls can point at different ids holding identical boxes.
const oldBlocks = new Map(borrowed('blocks.json').map(b => [b.name, b]))
const oldReportPath = path.join(reportsDir, '..', '..', '..', 'reports', 'generated', 'reports', 'blocks.json')
const collision = borrowed('blockCollisionShapes.json')
const geometryOf = name => {
  const v = collision.blocks[name]
  if (v === undefined) return undefined
  const ids = Array.isArray(v) ? v : [v]
  return JSON.stringify(ids.map(i => collision.shapes[String(i)]))
}
// One exemplar block per definition type, taken from the borrowed version.
const exemplar = new Map()
if (fs.existsSync(oldReportPath)) {
  const oldReport = JSON.parse(fs.readFileSync(oldReportPath, 'utf8'))
  const seen = new Map()
  for (const [name, entry] of Object.entries(oldReport)) {
    const short = bare(name)
    const g = geometryOf(short)
    if (g === undefined) continue
    const type = entry.definition?.type
    if (!seen.has(type)) seen.set(type, new Set())
    seen.get(type).add(g)
    if (!exemplar.has(type)) exemplar.set(type, short)
  }
  // Only keep a type as an exemplar if it really does pin one geometry.
  for (const [type, geoms] of seen) if (geoms.size !== 1) exemplar.delete(type)
}
const missing = []
const restated = []
const byType = []
const measured = []
const blocks = []
for (const [name, entry] of Object.entries(blockReport)) {
  const short = bare(name)
  const ids = entry.states.map(s => s.id)
  const def = entry.states.find(s => s.default)?.id ?? Math.min(...ids)
  let was = oldBlocks.get(short)
  let borrowedFrom = null
  if (!was) {
    const stand = exemplar.get(entry.definition?.type)
    if (stand) {
      was = oldBlocks.get(stand)
      borrowedFrom = stand
      byType.push(`${short} (as ${stand})`)
    } else if (MEASURED[short]) {
      const id = String(Object.keys(collision.shapes).length)
      collision.shapes[id] = MEASURED[short]
      collision.blocks[short] = Number(id)
      was = { boundingBox: 'block', minStateId: 0, maxStateId: 0 }
      borrowedFrom = 'measured'
      measured.push(short)
    } else {
      missing.push(`${short} [${(entry.definition?.type ?? '?').replace('minecraft:', '')}]`)
    }
  }
  if (was && !borrowedFrom && was.maxStateId - was.minStateId + 1 !== ids.length) restated.push(short)
  if (borrowedFrom && borrowedFrom !== 'measured') collision.blocks[short] = collision.blocks[borrowedFrom]
  blocks.push({
    id: blocks.length,
    name: short,
    minStateId: Math.min(...ids),
    maxStateId: Math.max(...ids),
    defaultState: def,
    // An unknown block is marked empty rather than solid. Both are guesses, but
    // walking through a wall is a visible failure and standing on air is the
    // one this client has been bitten by.
    boundingBox: was ? was.boundingBox : 'empty',
    states: entry.properties ?? {},
  })
}

const out = path.join(mcdataDir, 'pc', version)
fs.mkdirSync(out, { recursive: true })
const write = (f, v) => fs.writeFileSync(path.join(out, f), JSON.stringify(v))
write('version.json', { minecraftVersion: version, version: protocol, majorVersion: version })
write('entities.json', entities)
write('items.json', items)
write('blocks.json', blocks)
// Packet ids and the wire grammar are copied wholesale. Verified separately:
// between 26.1 and 26.2 not one packet id moved.
// Copied wholesale. Packet ids and the wire grammar are verified unchanged
// between the two versions before this is run; the rest are files genversion
// opens but does not use for anything version-critical.
for (const f of ['protocol.json', 'effects.json', 'enchantments.json', 'biomes.json',
                 'instruments.json', 'materials.json', 'particles.json', 'sounds.json',
                 'foods.json', 'language.json', 'recipes.json', 'tints.json']) {
  const from = path.join(mcdataDir, 'pc', borrow, f)
  if (fs.existsSync(from)) fs.copyFileSync(from, path.join(out, f))
}
// Written rather than copied: new blocks resolved by type were added to it.
write('blockCollisionShapes.json', collision)

console.error(
  `${version}: ${entities.length} entities, ${items.length} items, ${blocks.length} blocks ` +
    `written to ${out} (shapes borrowed from ${borrow})`)
if (byType.length) {
  console.error(`${version}: ${byType.length} new block(s) took their shape from an ` +
    `existing block of the same kind:\n  ${byType.join('\n  ')}`)
}
if (measured.length) {
  console.error(`${version}: ${measured.length} block(s) of a new kind used a shape ` +
    `measured against a running server: ${measured.join(', ')}`)
}
if (missing.length) {
  console.error(`${version}: ${missing.length} block(s) have no shape in ${borrow} and no ` +
    `same-kind block to take one from, so they are treated as empty — a bot ` +
    `will walk through them and not stand on them:\n  ${missing.join('\n  ')}`)
}
if (restated.length) {
  console.error(`${version}: ${restated.length} block(s) changed their state count since ` +
    `${borrow}, so a borrowed per-state shape array no longer lines up:\n  ${restated.join('\n  ')}`)
}
