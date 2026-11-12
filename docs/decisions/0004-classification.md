# 0004 — Classification

Status: accepted.

## Decision

Classification runs once at intake on the release name and the complete file
list, while the metadata is in memory, and its result is stored on the torrent
row. It is never recomputed: a classifier change applies to new torrents only.

The rules are ordered plain Go rules over embedded data — extension buckets,
keyword lists and the scene grammar — replacing bitmagnet's CEL workflow. The
keyword mini-language and the scene-token grammar are kept as knowledge; the
CEL environment, the protobuf payload, the action types and the database-attach
actions are not.

- The name is read first, on its own. Illegal content is blocked and explicit
  adult content is categorised without reading the file list at all.
- The file census decides everything else it can: a file list that is mostly
  video, audio, software, comic or ebook wins over the name. A release can be
  named anything, and a film name is no evidence against a list of executables.
- The name decides where the files cannot: archives, disk images, unknown
  extensions, and torrents with no file list.
- The complete file list is used, not the first hundred rows. The keyword scans
  read the name and a bounded prefix of paths — eight paths, two kilobytes —
  because markers live in names and folder names, not deep inside a list.
- The extension census reads every file. It is a linear pass with no regular
  expressions, so a torrent with thousands of files costs microseconds.

### Stored category

The stored category is a Newznab leaf where the classifier can justify one and
a parent otherwise. A parent category matches its leaves through a range filter,
so searching "Movies" finds every film regardless of resolution.

| content | category |
|---|---|
| film, resolution stated | 2030 SD, 2040 HD, 2045 UHD |
| film, resolution unknown | 2000 |
| television, resolution stated | 5030, 5040, 5045 |
| television, resolution unknown | 5000 |
| audio book | 3030 |
| music | 3000 |
| ebook | 7020 |
| comic | 7030 |
| game | 4050 |
| software | 4000 |
| adult | 6000 |
| other | 8000 |

### Junk and drops

Dropping is irreversible for the affected torrents, so only two things are
dropped, both conservatively and both recorded permanently in the blocklist:

- **Illegal content.** Strong markers block on their own. Weak markers — the
  word "underage", a release group called "LSM", the Spanish word "pedo" — need
  corroboration from a second, age-specific word. Adult words are not
  corroboration: "porn", "sex" and "xxx" are ordinary in legal adult releases.
  The film "Lolita", the novel of the same name and adult releases using Lolita
  as a performer name must not be deleted, and are not.
- **Deceptive content.** A software-dominant file list under a film or
  television release name is malware, not a misclassification. The bar is a
  release naming convention — a season or episode marker, or a stated resolution
  together with a source, codec or container marker — so a program named after
  a format ("Ebook Reader") or advertising a resolution ("4K Video Downloader")
  is a program, not a lie.

Adult content is categorised XXX and stored. A configuration option, off by
default, drops it instead; that decision is reversible and not a block.

A second configuration option drops torrents by writing system: `cjk`, `hangul`,
`cyrillic`, `arabic`, `hebrew`, `thai`, `devanagari`, `greek`, or `nonlatin` for
any dominant non-Latin script. It also only drops, never blocks, so turning it
off again applies to future torrents. The filter reads the dominant script of
the name, ignoring the extension and requiring a strict majority, so mixed names
such as "Batman 蝙蝠侠" are kept rather than guessed at. It is off by default:
which scripts are wanted is a property of the installation, not of the
classifier.

Nothing else is dropped: unclassified content is stored as Other.

### Not decided here

TMDB matching is not part of this step. The classifier parses no titles and
stores no content reference yet; `content_id` stays zero until the matching
step lands. Title and year parsing for it exist as a specification only.

## Why

The measured numbers, on 10,004 real torrents with file lists and 197,032 real
names from a live 9.75 M-row database:

- **Coverage 86.5 %** of the sampled torrents fall in a known category, against
  55 % classified in the same database by bitmagnet.
- **Agreement 87.4 %** with bitmagnet's content types on the rows it classified.
- **The file list changes the answer for 28 % of torrents.** Ignoring files
  drops coverage from 86 % to 60 %, which is close to the coverage bitmagnet
  has. That is the measurement behind "the files are evidence, not a hint".
- **Adult keyword coverage and precision:** the adult list covers acts, kinks
  and studios (about 110 weak markers, plus studios and codes that are strong).
  Of the rows it calls adult, 96.6 % are adult to bitmagnet as well; the
  disagreements are mainly adult releases bitmagnet types as films and a few
  mainstream titles that TMDB confirms or corrects.
- **The live-crawl review:** on 92 hand-labelled rows from a real crawl, 88 %
  are classified correctly after the review round described in the working
  notes; the remaining misses are mostly releases whose only signal is the
  title, or labels that are themselves uncertain.
- **Blocking precision was reviewed on real data.** On 197,032 random names the
  rules match 41 (0.02 %), all of them illegal on inspection. On 2,478
  prefiltered candidates they match 1,534, all illegal on inspection; the legal
  matches an earlier version produced — the film and novel "Lolita", a Nabokov
  audiobook, legal adult releases with performers named Lolita — were removed
  before this was written. The candidate set comes from a 9.75 M-row database;
  the full run over all of it happens in the importer step, where every match
  can be reviewed.
- **Deceptive releases are rare but real:** one in 10,004 sampled torrents is a
  software-dominant file list under a media name — in that case a 540 MB APK
  behind an adult video.

## Known limits

- Film, television and adult distinctions are name-based until TMDB matching
  lands. Adult keyword rules over-trigger on mainstream titles that contain
  words such as "sex": about 9 % of the films this classifier identifies are
  typed adult by bitmagnet.
- Japanese adult releases named only by a studio code, and titles written in
  CJK, are mostly Other. Bitmagnet has the same limit.
- Archives and disk images are opaque. A film claim shipped as a single archive
  cannot be verified, and a malicious archive cannot be detected without
  unpacking it, which is out of scope.
- The keyword scan is bounded at eight paths. A marker after the eighth file is
  not read.
- Roughly 15 % of sampled torrents have neither a decisive file list nor a
  naming convention and land in Other.
