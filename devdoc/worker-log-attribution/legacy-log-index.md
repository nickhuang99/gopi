<!-- doc-class: historical -->
# Legacy worker-log index — the disposition's evidence

Epic: **worker-log-session-attribution** (2026-09-12). This file is the
measured index behind the disposition table in
`devdoc/worker-log-session-attribution-20260912.md`.

Before the fix the launcher wrote `/home/user/.gopi/logs/worker-<ts>-<i>.log`: the name
carries a TIMESTAMP, not a task id, so the id→output path was broken. The
files are NOT deleted (deleting them would destroy the very evidence this epic
is about); this index makes their contents findable from a task id TODAY, even
though the names still carry none, and nothing new is ever written here.

## The 20 worker logs

| log file | bytes | last write | task/session ids named INSIDE the file (measured by content scan) |
|---|---:|---|---|
| `worker-20260911-042401-1.log` | 372673 | 2026-09-11 05:03 | `01a08c75-9d17-714a-9cb4-c2114ac0da79`, `01a08cfd-ada2-75a0-a167-fb39daffa47b`, `01a08d0f-dfdd-79e8-825a-71a73445495c`, `103e9818-77d7-402f-b6bf-e316dd8460b9`, `11111111-2222-3333-4444-555555555555`, `aaaaaaaa-0000-4000-8000-000000000002`, `b146b833-8728-4e5c-aa95-76af1c86e55f`, `d60e415e-a274-481d-a8f9-91c3b38d9d76` |
| `worker-20260911-042402-2.log` | 371 | 2026-09-11 04:25 | **(no id — pre-claim startup / broken-pipe noise)** |
| `worker-20260911-042403-3.log` | 371 | 2026-09-11 04:25 | **(no id — pre-claim startup / broken-pipe noise)** |
| `worker-20260911-043104-1.log` | 5900077 | 2026-09-11 13:58 | `t-1788060307`, `t-1788156772`, `t-1788176220`, `t-1788361069`, `t-1788397248`, `t-1788509224`, `t-1788767476`, `t-1788769211`, … (33 more) |
| `worker-20260911-043105-2.log` | 371 | 2026-09-11 04:32 | **(no id — pre-claim startup / broken-pipe noise)** |
| `worker-20260911-072424-1.log` | 3390869 | 2026-09-11 11:41 | `t-1788343430`, `t-1788361069`, `01a08d67-a7b3-7c72-90a5-2b4c3f3d16fc`, `01a08d70-2259-70c7-ac9d-192453cfc51e`, `01a08d78-3e00-7c12-a624-3991b5119075`, `01a08d8f-ceed-7c31-a59b-9f0893685333`, `01a08d94-82c0-76f6-8240-dc711f922aa1`, `01a08dcd-0401-7cd9-9c69-969876a43276`, … (16 more) |
| `worker-20260911-072425-2.log` | 4228461 | 2026-09-11 13:05 | `t-1788690836`, `t-1788818877`, `01a08d67-cc86-7bbb-8a3a-827a08ed9066`, `01a08d6e-bd24-7b18-8f62-6d02314a444d`, `01a08d94-01a8-79ba-9b1c-3f8c61675c0a`, `01a08d9a-649a-7193-8440-0d6a389bcda4`, `01a08daf-e99f-7394-85ff-f16f29a921be`, `01a08dcc-fef2-7a22-b47b-b48d7031a00b`, … (25 more) |
| `worker-20260912-032416-1.log` | 1173522 | 2026-09-12 04:12 | `01a091ea-f519-7404-a22e-6da85f579f99`, `01a091f1-5947-7c45-825f-8c35284bec68`, `05e1e61b-8a99-4c5e-83e9-dff3ff415824`, `eeb68308-15cb-4488-b995-b27de3b012ca` |
| `worker-20260912-032417-2.log` | 837729 | 2026-09-12 04:13 | `01a091ea-f649-79b5-8d64-a18eca4e7ca0`, `1c8831d2-de3b-42b9-b9c9-f76c948dac3c` |
| `worker-20260912-033016-1.log` | 1057606 | 2026-09-12 04:12 | `t-1788361069`, `t-1788905574`, `01a091eb-f575-7c4f-88a3-5fcec5d55d75`, `01a091f1-50ef-7118-91f0-d6affa67eca9`, `01a091f4-d73a-7651-8fc8-7a7c92374616`, `01a0920b-afe0-7587-ac78-57dddd117497`, `4e2a48d8-b7e8-42cb-bd49-88512b8fcd22`, `a728f9ee-2a0f-4c7b-994b-cd9258d8e51f`, … (1 more) |
| `worker-20260912-033017-2.log` | 332554 | 2026-09-12 04:16 | `01a091ea-f519-7404-a22e-6da85f579f99`, `01a091ed-5466-7b9c-a627-817ac1a0e994`, `8a8693fa-cc97-4674-8e7f-13a03a04a5d5`, `8cc49594-e260-4eef-8f23-3b9deb22834f` |
| `worker-20260912-033018-3.log` | 444671 | 2026-09-12 04:12 | `01a091ed-62a1-7ceb-be5f-adf64cf9cf1c`, `3ef61cfb-ee45-4d0b-b87d-11c1607f9913` |
| `worker-20260912-040617-1.log` | 124921 | 2026-09-12 04:12 | `01a08cfd-ada2-75a0-a167-fb39daffa47b`, `01a091f1-50ef-7118-91f0-d6affa67eca9`, `01a091f9-4e2b-73e0-8b66-f5c96754796c`, `89d62ead-acc2-4a33-aa33-de132ff5e399` |
| `worker-20260912-040618-2.log` | 98680 | 2026-09-12 04:13 | `01a091fb-1cff-7b83-9af3-47cfb2670865`, `f3901292-45ac-4460-941f-404eef7e2794` |
| `worker-20260912-040619-3.log` | 91739 | 2026-09-12 04:13 | `01a091fb-1e7d-7293-a493-22d3748d2f59`, `428610c1-cbc5-46e8-abe3-a8c07f203aa3` |
| `worker-20260912-042552-1.log` | 1297541 | 2026-09-12 06:30 | `t-1788087991`, `t-1788088846`, `t-1788301092`, `t-1788361069`, `01a08c4f-6bc0-7661-b471-b00f795d8063`, `01a091f1-50ef-7118-91f0-d6affa67eca9`, `01a0920b-17bb-7046-9f7f-de1bd03df56a`, `01a09215-e999-7158-a76e-0c0411d97797`, … (6 more) |
| `worker-20260912-042553-2.log` | 1500280 | 2026-09-12 06:30 | `01a091f1-5947-7c45-825f-8c35284bec68`, `01a091f4-d73a-7651-8fc8-7a7c92374616`, `01a091f9-4e2b-73e0-8b66-f5c96754796c`, `01a0920b-afe0-7587-ac78-57dddd117497`, `01a09216-6561-7213-9df9-c228e6f409c6`, `01a09248-1332-7672-8b05-d7c43cb14a29`, `01a0924c-5201-7570-b70e-fac7f5e2ed7a`, `01a09254-148f-762b-8d6e-0a9b2a943cc6`, … (4 more) |
| `worker-20260912-050215-1.log` | 1251530 | 2026-09-12 06:30 | `t-1789013949`, `01a091ed-5466-7b9c-a627-817ac1a0e994`, `01a091ed-62a1-7ceb-be5f-adf64cf9cf1c`, `01a0920b-17bb-7046-9f7f-de1bd03df56a`, `01a09225-a216-7453-ba0f-2513cf2f8a60`, `01a09254-148f-762b-8d6e-0a9b2a943cc6`, `01a09258-5d29-71a5-a361-b77aabe5fe41`, `01a0925d-a1a7-715e-a864-d75f361c3bbe`, … (3 more) |
| `worker-20260912-050216-2.log` | 1025699 | 2026-09-12 06:30 | `t-1788053263`, `t-1788847341`, `01a09216-6561-7213-9df9-c228e6f409c6`, `01a09237-f56a-7af6-8709-9a4825f1a2f4`, `01a09253-5506-7736-a111-22ed68578f69`, `01a09257-fc64-78a2-9253-a14a20a49c95`, `3757c763-eeaf-423a-8765-c10560866af7`, `9cd9d6db-9a9b-482d-9a4d-04ed023f0dc8`, … (1 more) |
| `worker-20260912-050217-3.log` | 1127146 | 2026-09-12 06:30 | `t-1788176220`, `t-1788384486`, `t-1788397248`, `01a08cd5-0212-7eac-83f8-665c3b825ec4`, `01a08cfd-ada2-75a0-a167-fb39daffa47b`, `01a091ea-f649-79b5-8d64-a18eca4e7ca0`, `01a091ed-5466-7b9c-a627-817ac1a0e994`, `01a091ed-62a1-7ceb-be5f-adf64cf9cf1c`, … (11 more) |

**Totals**: 20 files, 23.1 MiB — 17 name at least one task/session id inside
(`worker: claimed <id>` / `claimed: <id>` lines and child session ids), 3 are
startup stubs (371 bytes; no claim ever happened in them).

## Why findable-by-content-scan is still not attribution

The scan works, but it is a SEARCH, not a PATH: it needs this directory
present, it cannot be done from the id in one step, and it collides — two
workers writing in the same minute produce files that both mention rows, with
nothing in the NAME to tell them apart (the 04:12 incident: 6 workers, one
~70 s window). The fix replaces the search with a path —
`<gopi-dir>/sessions/<task-id>/output.log`, rendered by `gopi task show <id>`
— so the id alone is sufficient.
