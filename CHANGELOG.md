# Changelog

## [0.4.0](https://github.com/cacack/gedcom-cli/compare/v0.3.0...v0.4.0) (2026-11-24)


### Features

* list the distinct places in a file ([317a23e](https://github.com/cacack/gedcom-cli/commit/317a23eb6880ff269365708684862cf6370f2ef6))


### Bug Fixes

* report an empty FILE as empty in media check ([b95ef08](https://github.com/cacack/gedcom-cli/commit/b95ef08399b99b315f2c12787ee1087e56d4b68f))

## [0.3.0](https://github.com/cacack/gedcom-cli/compare/v0.2.0...v0.3.0) (2026-11-24)


### Features

* add a duplicates command for likely-same people across two files ([9b4aee0](https://github.com/cacack/gedcom-cli/commit/9b4aee0efe8966f454fb107cf6077ca81b309bfe))
* add an extract command that writes a branch of the tree to its own file ([d319229](https://github.com/cacack/gedcom-cli/commit/d319229c3a891b3e13fab3862f4679d4c7f6a0bc))
* add ancestors and descendants commands ([f27f3ed](https://github.com/cacack/gedcom-cli/commit/f27f3ed5a1ce51e10f6b58ce05456d19a1c8e335))
* add merge-people and merge-families ([ac4fa46](https://github.com/cacack/gedcom-cli/commit/ac4fa46c03ca37e8ce3328c6a7ecdd898242ccc1))
* add put and delete commands to replace, add or remove records ([fa30463](https://github.com/cacack/gedcom-cli/commit/fa3046391ff1ea383abd574790cca1014236ee03))
* **combine:** join GEDCOM files into one, zipping records they share ([8f2e34d](https://github.com/cacack/gedcom-cli/commit/8f2e34da9041581f467269982889ec4f6b7f9984))
* **convert:** rewrite a file as GEDCOM 5.5.1 or 7.0, in UTF-8 ([cd88117](https://github.com/cacack/gedcom-cli/commit/cd88117c0c8463fe5b0370e73149011fa8c2f1ae))
* **diff:** compare two versions of a file record by record ([60d2d7d](https://github.com/cacack/gedcom-cli/commit/60d2d7dc77290f116b195efcf017cf689b4da546))
* mark presumed-living people and redact them on request ([fc8d1b0](https://github.com/cacack/gedcom-cli/commit/fc8d1b033286d84d2973f3b45ee1fe8e25602f42))
* **media:** check FILE references and attach media to records ([81fb62d](https://github.com/cacack/gedcom-cli/commit/81fb62d0eaa9a9e6cb56c613d556983e84d70579))
* **relatives:** print a person with their parents, siblings, spouses and children ([dc5343e](https://github.com/cacack/gedcom-cli/commit/dc5343e155eb9e84c52d46ea3e1c6b59bd44b0ff))


### Bug Fixes

* apply duplicates --min-score to the printed score ([2b73b3f](https://github.com/cacack/gedcom-cli/commit/2b73b3f0fc17c38059e2999e3c5d3d13acc48fd5))
* **combine:** warn on dropped header values and qualify the one-file claim ([5cf1479](https://github.com/cacack/gedcom-cli/commit/5cf1479b4fadcabe59d9c659fa0de52dbfc9fe01))
* **combine:** write through the shared output path like the other write commands ([bbf5d6f](https://github.com/cacack/gedcom-cli/commit/bbf5d6f0bffc28d2bfbb5130132653c143864768))
* **convert:** report decode diagnostics and replace the output atomically ([340ddb1](https://github.com/cacack/gedcom-cli/commit/340ddb1c840c595aea59ca7e24c5e5da73ec605d))
* keep output permissions and tighten extract's stdin and stderr contract ([c42db80](https://github.com/cacack/gedcom-cli/commit/c42db80a0dc1e9580f76ed609f522f990b0c9e0f))
* **media:** follow a symlinked root and keep references inside it ([37504b5](https://github.com/cacack/gedcom-cli/commit/37504b5c16c9878698de3d4c5fb05ea49a9aaa52))
* **merge:** write through the shared output path like the other write commands ([e80a98d](https://github.com/cacack/gedcom-cli/commit/e80a98dd4e70782ce2080bb18c30b827a1dd041e))
* refuse duplicate xrefs in delete and guard the records file from -o ([20b3573](https://github.com/cacack/gedcom-cli/commit/20b35732e43e48d731bca94f9a9c96384237781a))
* **relatives:** honor --redact-living and report living ([c00af93](https://github.com/cacack/gedcom-cli/commit/c00af935ae01ef8c66927b5b70010df1e90c942d))
* **relatives:** ignore spouse links the family does not return ([f31a988](https://github.com/cacack/gedcom-cli/commit/f31a988051541b8d03284664f5bc7e3368aa6258))
* skip walk results that do not resolve to an individual ([70fb23d](https://github.com/cacack/gedcom-cli/commit/70fb23d74e7dab969b37d7c04cd6b906b72b20ba))
* state what --redact-living leaves visible ([8a23031](https://github.com/cacack/gedcom-cli/commit/8a230316c033400898aebf1ada6281b8a5a9795c))

## [0.2.0](https://github.com/cacack/gedcom-cli/compare/v0.1.0...v0.2.0) (2026-11-23)


### Features

* **get:** print records by xref ([70d82eb](https://github.com/cacack/gedcom-cli/commit/70d82eb42acd48b674d7a6045363909f4cf4174f))
* **list:** enumerate records of a type ([6ee5a3d](https://github.com/cacack/gedcom-cli/commit/6ee5a3d3f4fe70aabb0a6fb92d3644a57219dabd))
* **validate:** report decode diagnostics and validation issues ([054994d](https://github.com/cacack/gedcom-cli/commit/054994d2228e26b30d8f21f9b1da14759b8dfcac))

## 0.1.0 (2026-11-23)


### Features

* publish release binaries with release-please and GoReleaser ([5ea5dce](https://github.com/cacack/gedcom-cli/commit/5ea5dce7380bb452699f749246f759ee47a172e6)), closes [#6](https://github.com/cacack/gedcom-cli/issues/6)
