# cotw — COTW save reader (Go library)

Read-only parser for **theHunter: Call of the Wild** animal population saves. It
returns one struct with the whole save, with species, reserves and need zone
species already named from the installed game's own data files.

No UI, no CLI, no writes. One entry point:

```go
save, err := cotw.ParseSave(saveFolder, cotw.Options{})
```

Requires Go 1.27.4. No third-party dependencies.

## Install

```bash
go get cotw-pop-viewer            # module path
```

```go
import "cotw-pop-viewer"
```

## What you get

```go
type Save struct {
    Dir      string    // save folder that was read
    SteamID  string    // account folder name
    Game     GameInfo  // version/cache the names came from
    ParsedAt time.Time
    Reserves []*Reserve
    Warnings []string
    Trophies map[string]SpeciesTrophy  // resolved per-species trophy table
}

type Reserve struct {
    Index  int; Key, Name, File string
    AlreadyConverted bool; ReserveSeed uint32; AtypicalsCooldownTime uint16
    Species    []*SpeciesPopulation
    NeedZones  []*NeedZone
    TotalAnimals, TotalGroups int
    Err string
}

type SpeciesPopulation struct {
    Index int
    SpeciesKey, SpeciesName string   // "wild_boar", "WILD BOAR"
    Definition string                // "wild_boar_general", "moose_yukon", ...
    Known      bool
    NameHashID uint32
    InitialTotalScore float64; InitialNumAnimals int32
    Revision uint32; RepopulationSkipCounter uint16
    Groups []*Group
    AnimalCount, MaleCount, FemaleCount, GroupCount int
    MaxWeight, MaxScore float64
}

type Group struct {
    Index int; SpawnAreaID int32; ID uint32
    NeedZoneGuids []uint32   // one zone per schedule slot, in slot order
    Animals       []*Animal
    NeedZones     []*NeedZone // one entry per slot: the group's day, repeats included
    Slots         []GroupSlot // the group's day: slot -> zone + window
    SpawnArea     *ZoneGeometry // centre/extent of the group's spawn area
}

type GroupSlot struct {
    Slot         int8     // index into the species timetable (-1: unplaced)
    ZoneID       uint32
    Type         int32    // 1 feeding, 2 drinking, 3 resting
    Discovered   bool
    HasWindow    bool
    StartHour, EndHour float64
    HourSource   string   // options | save | learned | wiki
    // the window the save stored for a discovered zone, which names the slot
    // of the group that discovered it, not necessarily this slot
    RecordedStartHour, RecordedEndHour float64
}

type Animal struct {
    GenderRaw uint8; Gender string     // 1/"male", 2/"female"
    Weight, Score float64
    FeatureFlags uint8; IsScripted bool
    VisualVariationSeed, ID uint32
    MapX, MapY float64
    // Trophy rating:
    Tier TrophyTier                    // medal this animal's Score rates
    IsDiamond, IsGold, IsSilver, IsBronze, IsGreatOne, IsNone bool
}

type NeedZone struct {
    ReserveIndex int
    ID int32; IDU uint32
    Type int32               // stored activity id; 1, 2 and 3 occur
    Position Vec3            // world metres; Y is elevation
    MapIconID uint64
    StartHour, EndHour float64
    Windows []HourWindow     // the window of the slot this group uses it at
    HourSource string        // options | save | learned | wiki
    AnimalTypeHash uint32
    AnimalSpeciesKey, AnimalSpeciesName string
    ScheduleIndex int8       // the slot in this group's path
    DiscoveredGroups []uint32
    Groups []GroupRef        // groups that use this zone
}
```

Helpers: `save.Summarize()`, `save.Reserve(index)`, `save.GameData()`,
`game.Species()`, `game.SpeciesByKey()`, `game.ReserveKeys()`,
`game.ReserveName()`, `game.PopulationDefinition()`, `game.String()`.
Zone geometry: `save.Zones(reserveIndex)`, `save.SpeciesSchedules()`,
`game.ZoneGeometry(id)`, `game.ZoneCandidates(id)`, `game.SpawnAreaGeometry(id)`,
`game.ZoneGeometries()`.

## Game data and the cache

`ParseSave` opens the game data first (`cotw.OpenGame`). Names live nowhere in
the save — only 32 bit hashes of them — so the library reads the installed game:

| source | provides |
| --- | --- |
| `StringLookup` assets in `archives_win64/game*.arc` | hash → localized text, 10 languages |
| XLS data books (`game72.arc`) | species keys (one sheet per species), wildlife codex ids, reserve keys |
| name-shape hashing (see below) | population definition ids → species |

The extraction is cached:

```
%LOCALAPPDATA%\cotw-save-reader\gamedata.json      (Windows; os.UserCacheDir elsewhere)
```

**Version check on every startup.** `OpenGame` derives a version string from, in
order: the install's `versions.*.txt` stamp, the Steam `appmanifest_518790.acf`
build id, or a signature over the archive files (name + size + mtime). If it
differs from the cached one — or the cache is missing/unreadable — the archives
are re-scanned and the cache rewritten. A first scan reads only the ADF entries
(a few hundred per archive, not the 117 GB of payload) and takes about 3 minutes;
later starts load the cache in well under a second.

```go
g, err := cotw.OpenGame(cotw.Options{
    InstallDir:   "",   // default: found via the Steam library folders
    CachePath:    "",   // default: user cache folder
    Language:     0,    // 0 en, 1 fr, 2 de, 3 es, 4 ru, 5 pl, 6 ja, 7 pt-BR, 8 cs, 9 zh
    ForceRebuild: false,
    Progress:     func(p cotw.Progress) { log.Printf("%d/%d %s", p.ArcsDone, p.ArcsTotal, p.Arc) },
})
fmt.Println(g.Info().Version, g.Info().CacheRebuilt)
```

Other options: `DisableDeepDefinitionSearch` (skip the word scan, unresolved
species stay `Known=false`), `ReserveIndexKeys` (extend the file-number → reserve
key table).

## How population ids resolve

A population entry's `NameHashId` is `hash32("<definition name>_population")`,
where the definition is the data asset that reserve uses for that species. A
species has several definitions, so the id varies by reserve even though the
species does not:

```
wild_boar_population              (default)
wild_boar_general_population      (Hirschfelden and others)
moose_yukon_population            (Yukon's override, shared with two other reserves)
lynx_population                   (legacy short name for eurasian_lynx)
```

Those names are not stored as strings anywhere in the shipped files, so the
library hashes candidate names in two stages:

1. at build time: every species key and its derived shapes (compact form, last
   word, last two words, key minus a trailing `_deer`/`_elk`/…) with and without
   the `general`-style tags — cached, covers most ids;
2. on demand: `<species key>_<word>_population` for every species key crossed
   with the words the game's own localization uses (about two million hashes,
   roughly a second), for the regional names. Whatever it finds is written back
   into the cache, so it is a one-time cost per game version.

Anything still unresolved is reported with `Known=false` and the raw hash; the
data for that population is still fully parsed.

## Reserve numbering

The save names reserves by file number (`animal_population_0` … `_20`). The order
is the game's own: the codex `Regions` sheet (XLSBook in `game72.arc` at offset
153419776) lists every reserve as a `reserve_id` in a fixed order —

```
hirschfelden, layton_lake_district, siberia, africa, patagonia, yukon, iberia,
rockies, nz, mexico, delta, finland, nh, australia, nepal, sh, alberta, scotland, peru
```

— and the save numbers follow it with the three **trophy lodge worlds** inserted
at 5, 7 and 15. So: 0–4 as listed, 5 = Spring Creek Manor (lodge), 6 yukon, 7 =
Saseka Safari Lodge (lodge), 8–14 as listed, 15 = Layton Lakes Trophy Cabin
(lodge), 16–20 as listed, 21 = peru (a reserve this account does not own).

Lodge numbers deliberately have no reserve key; `cotw.TrophyLodgeWorlds` names
them. The table lives in `population.go` (`reserveIndexKeys`); names for those
keys still come from the game's localization. Use `Options.ReserveIndexKeys` to
add or override entries (e.g. to add `21: "peru"` once that DLC is owned).

## Trophy tiers

Every `Animal` carries the medal its `Score` rates, as one bool per tier plus a
`Tier` enum. The rule is the game's own, confirmed against this save's harvest
history and the game's documentation:

| Trophy Rating | Medal |
| --- | --- |
| below 20% of the species maximum | BRONZE |
| 20% – 60% | SILVER |
| 60% – 90% | GOLD |
| 90% – 100% | DIAMOND |
| beyond the species maximum | GREAT ONE |
| no trophy organ (a female of an antlered species) | NONE |

A tier is therefore a band of the **species' maximum trophy rating** — not of
weight, and not of the population's current best score. Which species carry the
organ in only one sex is read from the installed game on every cache rebuild —
the `Trophy type` column of its per-species organ sheets is `male` or `both` — so
a female of an antlered species is rated NONE, and that list refreshes itself
when a patch adds or changes a species.

**What survives a game update:** the organ-ownership set is re-read from the
install whenever the version check rebuilds the cache, and the animal, species
and name data always has been. The band fractions (20/60/90 %) are the game's
rule and are compile-time constants here. The per-species maxima cannot be
re-derived — see below — so a patch that rebalances a species' trophy cap will
not be noticed. If that matters to you, pass `Options.TrophyMaximums` with
corrected values; it takes precedence over the built-in table.

The one input the game does **not** ship as data is the per-species maximum —
it is computed from each species' trophy model inside the executable (it appears
in no XLS book, no archive asset, and no float table in the exe). The library
therefore carries documented community values for the species the game wiki
covers, validated against this save: for all 84 rated harvests in it, the
computed medal is never worse than the medal the game recorded. Species with no
documented maximum get `TierUnknown` and all-false bools.

Supply or correct values through the option:

```go
save, err := cotw.ParseSave("", cotw.Options{
    TrophyMaximums: map[string]float64{"banteng": 132.6},
})
```

`save.Trophies` exposes the resolved table (`Maximum`, `MaleOnly`, `Source`),
and `SpeciesTrophy.Thresholds()` returns the four score cut-offs.

Two things to keep in mind: the game downgrades the medal by **one level** when
the harvest check fails (wrong ammo class, too many shots, damaged trophy organ,
no vital hit), so a *computed* tier is the best the animal could have earned and
the recorded one may be one worse; and a rating above the species maximum is
reported as GREAT ONE, which is where genuine Great Ones sit.

## Need zones and spawn areas

The save only records need zones the player has discovered, but the game ships
the geometry for **all** of them. It is not stored in a need-zone asset: it lives
in the reserve spawn bitmaps (`bitmap_data.adf`), as layers of rectangular
`BitmapArea` records:

```
BitmapArea: key u32, X u32, Y u32, Size u32, Tiles[], AabbMin[3]f32, AabbMax[3]f32
```

- **Layer 0** holds the spawn areas: each `key` is a `Group.SpawnAreaID`, so
  every group gets a position without discovering anything.
- **Layers 1–3** hold the need zones, one layer per need zone type; each `key`
  is the zone id a group lists in `NeedZoneGuids`.
- The bounding box is the zone's volume, so the box centre is the zone position.

Verified on a real save: 434,845 zone geometries and 5,8xx spawn areas are
shipped, **every** one of the 15,979 groups resolves to a spawn-area position,
and all 171 discovered zones match exactly — same type, position equal to 0.0 m.

```go
for _, zi := range save.Zones(0) {   // every zone of the reserve, found or not
    fmt.Println(zi.SpeciesKey, zi.Type, zi.Position, zi.StartHour, zi.EndHour,
        zi.Discovered, zi.HasGeometry)
}
```

A few things worth knowing:

- **A group's `NeedZones` is complete, one entry per slot.** Every id in
  `NeedZoneGuids` gets an entry: the discovered ones come from
  `found_need_zones_adf` with `Discovered=true`, the rest are filled from the
  shipped geometry (`Discovered=false`) with the type, position and the window of
  the slot the group uses the zone at. So
  `save.Reserves[i].Species[j].Groups[k].NeedZones` prints a real list even on a
  reserve you have never scouted, and both `NeedZone` and `ZoneInfo` render
  themselves for `fmt`.
- **A repeated zone appears once per slot.** Many groups return to the same zone
  later in their day (7,522 of 15,980 groups on the reference save use one zone at
  two slots), and the list keeps one entry per use, each with its own
  `Slot`/window. Deduplicating by zone id would list a 7-slot group as 6 zones and
  lose the earlier window - the second slot overwrote the first. Read a group's day
  from `NeedZones` in order, or from `Slots`, which is the same thing keyed by slot
  index; a zone's own record under `Reserve.NeedZones` stays one per file entry and
  keeps the window the save recorded.
- **Zone ids are not unique across reserves.** About 5,800 ids appear in more
  than one reserve's layout at genuinely different places. `save.Zones` resolves
  those by picking the candidate nearest the spawn areas of the groups that use
  the zone; `game.ZoneCandidates(id)` exposes all of them.
- **Which zone is used when** is `Group.Slots`; see "Need zone hours" below for
  where the windows come from, because the game does not ship them.

The geometry does not live in the JSON cache — it is far too large — but in a
compact binary sidecar next to it (`gamedata.json.zones`, 32 bytes per record),
which is written and versioned with the cache.

## Need zone hours

The game does **not** ship a need-zone timetable. Checked exhaustively against
this install: all 17 design workbooks (every sheet listed), all 37,052 parseable
assets searched by name-table token, and the reserve bitmaps. The only need-zone
field anywhere in shipped data is `NeedZoneToxicThreshold` (hunting pressure).
The engine owns the windows and writes them into a save *when a zone is
discovered* (`NeedZoneStartTimeHours`, `NeedZoneEndTimeHours`,
`NeedZoneScheduleIndex`).

### Which zone a group is at, and when

A zone is **not** active at every window of its activity type. A group has one
zone per slot of its species' timetable and is at that zone during that slot, so
three resting zones of one herd belong to three different rest periods, exactly
as the community pages describe.

The slot order is the order of the group's zone path. `NeedZoneGuids` lists the
group's zones **in slot order**: entry *i* is the zone used during slot *i*. That
is verifiable on a real save without the engine:

- for every zone only one group uses, the `NeedZoneScheduleIndex` the save
  recorded for it equals the position it occupies in that group's path
  (94 of 94 on the reference save);
- for every group, the activity type the shipped geometry gives the zone at each
  position equals the species timetable's type at that position (104,270 of
  104,270 group entries).

So a discovered zone's recorded window names the slot of the group that
discovered it, which for a zone several herds share (about a third of them) is
not necessarily the slot another herd uses it at. `GroupSlot` therefore carries
both: the slot's window and, for a discovered zone,
`RecordedStartHour`/`RecordedEndHour`.

```go
for _, sp := range reserve.Species {
    for _, g := range sp.Groups {
        for _, s := range g.Slots {   // the group's day, in slot order
            fmt.Printf("%s %02.0f:00-%02.0f:00 zone %d (%s)\n",
                sp.SpeciesName, s.StartHour, s.EndHour, s.ZoneID, ZoneTypeName(s.Type))
        }
    }
}
```

The window of each slot is resolved from the best source available, slot by
slot. The order matters: a timetable is per reserve, so the reserve's own
evidence and the page for that reserve come before anything species-wide -
otherwise a variant discovered on one reserve (or the species-wide entry the
learned store keeps) leaks into another reserve's schedule.

| Order | Source | `HourSource` |
|---|---|---|
| 1 | `Options.ZoneHours` (key `species` or `reserve/species`) | `options` |
| 2 | this reserve's discovered zones | `save` |
| 3 | an earlier parse's entry for this reserve | `learned` |
| 4 | the published page for this reserve (its variant, or the row it shows) | `wiki` |
| 5 | this save's discoveries on other reserves, where every reserve agreed | `save` |
| 6 | the learned store's species-wide entry, while no reserve contradicts it | `learned` |
| — | nothing known | `slot` is `-1`, no window |

`Save.Timetable(reserve, species)` returns that merged table directly, and
`ZoneInfo.Slots` lists the slots a zone is used at (more than one for a shared
zone).

Where no timetable covers a slot at all (a species the published table does not
cover, such as mink or woodcock, with no discovery to learn from), a discovered
zone's slot keeps the window the save recorded for that zone: the best source
there is, even though it names the slot the player found the zone in. On the
reference save that leaves 7 of 29,785 (group, activity) pairs showing two zones
at one window, all of them those species.

**Learning.** Every parse records what the save revealed in
`gamedata.json.hours` (beside the cache, not inside it: the cache comes from the
installed game, this comes from save files). A species discovered once keeps its
windows for every later save, and a species discovered on one reserve covers
other reserves — unless two reserves are known to disagree, in which case only
the reserve-specific entry is used. Set `Options.DisableScheduleLearning` to
keep the library from writing the store. A store written by a different game
version is ignored (and reported in `Save.Warnings`), because a patch may have
changed the timetable.

**Published fallback.** `zonehours.go` carries a transcribed table covering 100
of the 130 species, from the community zone-times pages
(`thehuntercotw.wordpress.com`, 15 reserve pages, fetched 2026-11-19). A page is
the timetable *for that reserve*, so where a page differs the table keeps a
reserve-specific variant, and every species a page lists is checked against it:
the resolved timetable for 112 of 112 (reserve, species) pairs this save has on
a paged reserve is exactly the page's windows (see
`TestPublishedHoursMatchEveryPage`). Its slot numbering was verified against
discovered zones: a slot's index is its rank by start hour (164/169 exact), and
157 of 158 comparable discovered windows agree with the page for their reserve.
`PublishedZoneHours(key)` and `PublishedZoneHoursFor(reserve, key)` expose it
directly.

Reserves with no page yet (Scotland, Alberta, Peru) fall back to the species rows
the pages give elsewhere. Scotland's fallow deer is carried as a variant typed to
the shipped geometry, which puts a drinking zone before the midday feeding zone
there.

Where a page's slot order still disagrees with the shipped geometry - the types
the game actually places at each path position - the zone's own activity decides:
the timetable is then assigned by activity type rather than by position, so each
zone still gets a window of its type. That check is also what catches a
transcription slip. On a real save 61,998 of 65,518 zone records get a window
(7,336 from this save, 54,662 from the published table); the 3,520 without one
are species no page covers (mink, pine marten, red grouse, badger, woodcock,
desert bighorn, woodland caribou) plus zone types a species genuinely lacks
(snow leopard has no drinking zones, European rabbit no resting zones).

## Format notes worth knowing

- **Need zone `Type`** is the stored activity id: `1` feeding, `2` drinking,
  `3` resting (`ZoneTypeName`). The file stores the number only; the names come
  from matching the published timetables, which agree on 157 of 158 discovered
  windows.
- **Only discovered zones exist** in `found_need_zones_adf`; a group's
  `NeedZoneGuids` also lists zones you have not found yet.
- **Zone ids are not unique within a reserve** (two entries can share one), and a
  group can list an id twice. Links are deduplicated; every entry with a
  referenced id is linked.
- **`NeedType`, `ID` are signed in the file, `NeedZoneGuids` are unsigned** —
  compare through `NeedZone.IDU`.
- **No Great One flag.** Current `AnimalData` versions have no `IsGreatOne` field
  (older tools read one that no longer exists), so the struct does not claim one.
- **`VisualVariationSeed`** is a random per-animal seed in current saves; the
  pre-2023 "seed → fur" tables no longer apply, so no fur is derived.

## Layout

```
doc.go            package documentation
model.go          Save / Reserve / SpeciesPopulation / Group / Animal / NeedZone
save.go           ParseSave, Summarize
container.go      SAVE/COMP + zlib unwrapping
steam.go          Steam install + save folder discovery, game version detection
game.go           OpenGame, cache read/write, species + definition tables
scan.go           archive (.tab/.arc) walk, StringLookup + XLS extraction
databook.go       XLSBook sheet/cell decoding
naming.go         species name shapes, population definition hashing
population.go     reserve file decoding
needzones.go      need zone decoding and group linking
reserves.go       file number → reserve key table
zones.go          shipped zone/spawn-area geometry, candidate lookup
zoneview.go       Save.Zones, timetables, zone -> reserve resolution
groupslots.go     Save.Timetable, per-group slot assignment (Group.Slots)
zonecache.go      binary geometry sidecar (gamedata.json.zones)
zonehours.go      learned schedule store + published fallback table
trophy.go         per-tier bools from species maximum ratings
adf/              read-only ADF container parser (+ lookup3 hash)
```

## Verification

Checked against the original Python (`deca/ff_adf.py`) parser on a real save:
11 populated reserves, 15,979 groups and 41,734 animals compared field by field,
plus all 171 discovered need zones — every value matched. On that save all 92
distinct population ids resolved to a species once the definition name shapes
above were applied.