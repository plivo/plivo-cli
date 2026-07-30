---
name: plivo-cx-agents
description: Build, update and publish Plivo CX Agents (conversation flows made of nodes) through the public Agents API. Trigger whenever the user wants to create, edit, wire, validate or publish a Plivo CX agent or flow, asks which Plivo nodes to use, or mentions AgentFlowNode / agent_uuid / the Agents API.
---

# Plivo CX Agents skill

A Plivo CX **Agent** is a directed graph: `nodes` (what happens) plus `connections` (what happens next). You build one by POSTing that graph as JSON. This file exists because the graph has a handful of non-obvious rules, and getting any one of them wrong produces an agent that returns `201 Created` and then does not work.

## If you are an AI agent — the rules that actually break flows

Read these eight. Everything else you can discover from the API.

1. **Never invent node config.** `GET /AgentFlowNode/{node_type}/` returns the node's JSON Schema *and a working example*. Read it and copy its shape. Guessing field names is the single most common failure.
2. **You do not supply node `id` or position.** Send `name`, `type` and `config`; the server mints the id and lays out the canvas. Ids are `<type>_<n>`, numbered per type in the order you sent them: `start_1`, `send_message_1`, `send_message_2`. Your `connections` must use those names — so either predict them from that rule, or create the agent first and read the ids back before wiring.
3. **Every node must be reachable by a connection.** Membership in a flow version is derived from the connection graph, not from the `nodes` array. A node no connection points at is **not part of the flow**. The API rejects this with `422` and names the orphans.
4. **Connections use node `id`. Variables use node `name`.** Different namespaces. Mixing them is the most expensive mistake here because it fails at *runtime*, not on save. See below.
5. **A connection endpoint is `<node_id>.<handle>`**, never a bare node id. Target handle is `Input`.
6. **Nothing validates a source handle — not the API, not save time.** A misspelled or invented handle returns `201`, round-trips through `GET` unchanged, publishes happily, and is a permanently dead branch. This is the one mistake with no server-side safety net, which is why the preflight check below verifies handles against the live catalogue. **Do not skip it, especially on a branching flow.**
7. **Once minted, an id is permanent.** On update, send every node back with the `id` it came back with. A node sent *without* an id is treated as new and gets the next free number; changing an existing id rewires or orphans the graph.
8. **`config.model` is flat on write, but not always flat on read.** Post it flat. Some node types (e.g. `http_request`) return it *nested under a key named after the node type*, and the server also injects a `name` key you did not send. Posting back exactly what you read still works — that is guaranteed — but do not assume the read shape equals the write shape when diffing.

Also: send `Content-Type: application/json`, keep the **trailing slash** on collection paths, and note `limit` is capped at 20 — a larger value is silently clamped to 20 rather than rejected, so check `meta.limit` if the page size matters to you.

## The workflow that works

```
1. GET /AgentFlowNode/                     → which node types exist
2. GET /AgentFlowNode/{type}/              → schema + working example, per node you need
3. build the graph                     → names, types, config, connections
4. run the preflight check (below)     → orphans, namespaces, and real handles
5. POST /AgentFlow/                        → 201, returns agent_uuid
6. GET /AgentFlow/{agent_uuid}/              → confirm nodes + connections came back
7. POST /AgentFlow/{agent_uuid}/Publish      → DRAFT → ACTIVE
```

Do not skip step 4. Steps 5 and 6 will happily report success on a flow with a dead branch or a variable that resolves to nothing — the preflight is the only thing that catches those.

## Auth and base URL

HTTP Basic, `auth_id:auth_token`. The `auth_id` in the path **must** equal the one you authenticate with, or you get `401`.

```bash
export A=YOUR_AUTH_ID
export T=YOUR_AUTH_TOKEN
export B=https://api.plivo.com/v1/Account/$A
```

## Endpoints

| Method | Path | Result |
|---|---|---|
| GET | `/AgentFlow/` | list; `?limit=` (clamped to 20), `?offset=`, `?state=DRAFT\|ACTIVE\|PAUSED` |
| POST | `/AgentFlow/` | `201` → `{api_id, message:"agent created", agent_uuid, name, resource_uri}` |
| GET | `/AgentFlow/{agent_uuid}/` | detail incl. `nodes` + `connections` |
| POST | `/AgentFlow/{agent_uuid}/` | update; `202` → `{api_id, message:"changed"}` |
| DELETE | `/AgentFlow/{agent_uuid}/` | `204` |
| POST | `/AgentFlow/{agent_uuid}/Publish` | `202` → `{api_id, message:"changed"}`; `DRAFT` → `ACTIVE`. **Empty body.** |
| POST | `/AgentFlow/{agent_uuid}/Pause` | `202`; → `PAUSED`. Empty body. |
| POST | `/AgentFlow/{agent_uuid}/Resume` | `202`; → `ACTIVE`. Empty body. |
| GET | `/AgentFlow/{agent_uuid}/Run/` | run history |
| GET | `/AgentFlow/{agent_uuid}/Run/{run_uuid}/` | one run + execution logs |
| GET | `/AgentFlowNode/` | all node types; `?types=a,b,c` fetches several schemas in one call |
| GET | `/AgentFlowNode/{node_type}/` | schema + examples for one type |

The resource key is **`agent_uuid`** in every response, including list rows — not a bare `id`. Run ids are
`run_uuid`. Every object also carries a `resource_uri`.

Timestamps are `YYYY-MM-DD HH:MM:SS.ffffff+00:00` (space-separated, numeric offset) — the same shape the rest of
the Plivo API uses, and the shape its datetime filters accept. Not ISO-8601 with `T`/`Z`.

## Node shape

What you send:
```json
{
  "name": "Send Message",
  "type": "send_message",
  "config": { "model": { "...": "per-node, from GET /AgentFlowNode/{type}/" } }
}
```
What comes back, with the server's additions:
```json
{
  "id": "send_message_1",
  "name": "Send Message",
  "type": "send_message",
  "left": 440, "top": 200,
  "config": { "model": { "..." : "..." } }
}
```

- `id` — **omit it**; the server mints `<type>_<n>` per type. Supply one only if you are managing ids yourself, and never change one that already exists.
- `name` — human label, **and the namespace variables use**.
- `type` — must be an exact `node_type` from the catalogue.
- `left` / `top` — canvas coordinates. **Omit them too**; the server lays the flow out left to right. Supply them only to control the layout.
- `config.model` — a flat dict of the node's fields **when you write it**. See Rule 8: the read shape can differ.

## Rule 4, expanded — the namespace trap

Connections address nodes by **id**. Template variables address them by **name**.

```json
{ "nodes": [
    { "name": "Start", "type": "start", ... },
    { "name": "Send Message", "type": "send_message",
      "config": { "model": {
        "to": ["{{Start.message.from}}"]        ← name  ✅
      } } } ],
  "connections": [
    { "source": "start_1.message", "target": "send_message_1.Input" }   ← ids  ✅
  ] }
```

The ids in `connections` are the ones the server will mint from the node types
above, in order. The names in `{{...}}` are the `name` fields.

```
❌ "to": ["{{send_message_1.message.from}}"]  id in a variable — saves fine, resolves to nothing at runtime
❌ "source": "Start.message"                  name in a connection — 400 or a broken graph
```

If you rename a node, every variable referencing it must change too. This is a good reason to set `name` once and leave it.

**Which trigger fields exist is not published.** The node schemas do not enumerate the trigger payload, so the only variable paths you can rely on are the ones that appear in the served examples — `{{Start.message.from}}`, `{{Start.call.header1}}`, `{{Start.outbound_call.to}}`. If you need a field you have not seen in an example (a message's body text, for instance), **do not invent a path**. Either read it off a real run via `GET /AgentFlow/{agent_uuid}/Run/{run_uuid}/`, or ask the user to confirm it from the console's variable picker. An invented path saves cleanly and silently resolves to nothing.

There is also a separate `{{secrets.NAME}}` namespace for stored credentials — use it instead of putting a token in `config`.

## Handles

Target handle is always `Input`. A source handle comes from **one of two places**, and missing the second one is the classic branching bug:

1. **Static** — an `id` from the source node's `output_states`, as returned by `GET /AgentFlowNode/{type}/`.
2. **Dynamic — minted by the node's own config.** A `branch` node names one handle per condition, using that condition's `alias`. Those aliases are legal source handles even though they never appear in `output_states`.

```json
{ "id": "branch-1", "name": "Is VIP caller?", "type": "branch",
  "config": { "model": { "conditions": [
      { "alias": "vip_caller", "expressions": [ ... ] } ] } } }
```
```json
{ "source": "branch-1.vip_caller", "target": "prompt-vip.Input" },   ← the alias
{ "source": "branch-1.no_match",   "target": "prompt-def.Input" }    ← an output_state
```

So `branch`'s valid handles are `no_match`, `error`, **plus every `conditions[].alias` you defined**. If you rename an alias, the connection using it must change too or that branch goes dead.

| Node | Source handles (`output_states[].id`) |
|---|---|
| `start` | `message`, `call`, `chat`, `whatsapp`, `email`, `http`, `outbound_call`, `outbound_message`, `outbound_whatsapp`, `outbound_email`, `destination`, `call_hangup`, `incoming_slack` |
| `send_message` | `sent`, `failed` |
| `http_request` | `success`, `failed` |
| `ai_agent_v2` | `sent`, `error` |
| `queue_and_route` | `completed`, `timeout`, `enqueue_failed`, `assignment_failed`, plus DTMF digits |

Four types have **no** output states at all (`ai_action`, `end_conversation`, `hangup`, `queue`) — nothing can lead out of them, so they are terminal. Anything else: read `output_states` from the catalogue. Do not guess.

**The Start trigger you pick decides the agent's type.** `message` → an SMS-triggered agent; `http` → an API-triggered agent you can fire programmatically. The backend derives this for you; you only choose the handle. Keep `config.model.triggers` on the Start node in agreement with the handle its connection uses.

To branch, give one node two outgoing connections on different handles, and offset the targets vertically so they do not overlap:

```json
"nodes": [
  { "id": "http-1", "left": 440, "top": 180, ... },
  { "id": "ok-1",   "left": 800, "top":  80, ... },
  { "id": "oops-1", "left": 800, "top": 320, ... } ],
"connections": [
  { "source": "http-1.success", "target": "ok-1.Input" },
  { "source": "http-1.failed",  "target": "oops-1.Input" } ]
```

## Node catalogue at a glance

52 types. Use `GET /AgentFlowNode/` for the live list; this is orientation only.

| Category | Types |
|---|---|
| Flow Trigger | `start` |
| Messaging | `send_message`, `send_message_and_wait_reply`, `send_whatsapp`, `send_whatsapp_and_wait_reply`, `send_email`, `send_chat`, `send_chat_and_wait_reply`, `message_conversation`, `whatsapp_conversation`, `email_conversation`, `chat_menu_button` |
| AI Agent | `ai_agent_v2`, `ai_agent_call`, `ai_agent_chat`, `ai_agent_message`, `ai_agent_whatsapp`, `ai_chatbot`, `ai_assist`, `ai_action`, `ai_skills`, `agent_node`, `agent_preset_conversation`, `contact_screening` |
| Voice & Telephony | `initiate_call`, `call_forward`, `ivr_menu`, `get_input`, `prompt`, `hangup`, `voicemail`, `conference_bridge`, `multi_party_call`, `multi_party_call_v2`, `multi_party_call_outbound`, `stream`, `waiting_room`, `customer_feedback_call` |
| Routing & Queueing | `queue`, `queue_and_route`, `agent_assignment`, `business_hour` |
| Flow Control | `branch`, `counter`, `set_attribute`, `end_conversation` |
| Integrations & Data | `http_request`, `api_integration`, `get_object`, `create_task` |
| Feedback & Input | `user_input`, `customer_feedback` |

There is no `branch_v2` — the type is `branch`.

## Worked example — SMS auto-reply

Two nodes, one connection, no ids and no coordinates. Verified end to end.

```bash
curl -s -u "$A:$T" -H "Content-Type: application/json" -d '{
  "name": "SMS auto-reply",
  "description": "replies to any inbound SMS",
  "nodes": [
    { "name": "Start", "type": "start",
      "config": { "model": { "triggers": ["message"] } } },
    { "name": "Send Message", "type": "send_message",
      "config": { "model": {
        "from": "YOUR_PLIVO_NUMBER",
        "to": ["{{Start.message.from}}"],
        "text": "Thanks for your message, we will be right with you." } } }
  ],
  "connections": [
    { "source": "start_1.message", "target": "send_message_1.Input" }
  ] }' "$B/AgentFlow/"
```

`start_1` and `send_message_1` are the ids the server mints for those two node
types, in that order. Read them back with `GET /AgentFlow/{agent_uuid}/` if you would
rather confirm than predict.

Then publish it:

```bash
curl -s -X POST -u "$A:$T" "$B/AgentFlow/AGENT_ID/Publish"
```

Note `config.model.triggers` on the Start node matches the handle used in the connection (`message`). Keep those two in agreement.

## Preflight check — run this before every POST

Catches the failures that a `201` will not tell you about. No dependencies.

```bash
python3 - flow.json <<'PY'
import json, os, re, sys, urllib.request, base64
d = json.load(open(sys.argv[1]))
nodes, conns = d.get("nodes") or [], d.get("connections") or []
err, warn, note = [], [], []

# Mint the ids the SERVER will mint, using its rule: <type>_<n>, numbered per
# type in payload order, and never overriding an id you supplied. Without this
# the checks below would compare your connections against ids that do not exist
# yet and call every node an orphan -- which is exactly the bug this mirrors.
def mint(nodes):
    taken = {n.get("id") for n in nodes if isinstance(n, dict) and n.get("id")}
    counters, out = {}, []
    for n in nodes:
        if not isinstance(n, dict):
            out.append(n); continue
        n = dict(n)
        if not n.get("id"):
            base = re.sub(r"[^a-z0-9_]+", "_", str(n.get("type") or "node").strip().lower()).strip("_") or "node"
            k = counters.get(base, 0) + 1
            cand = f"{base}_{k}"
            while cand in taken:
                k += 1; cand = f"{base}_{k}"
            counters[base] = k; n["id"] = cand; taken.add(cand)
        out.append(n)
    return out

nodes = mint(nodes)
ids   = [n.get("id") for n in nodes if isinstance(n, dict)]
names = {n.get("name") for n in nodes if isinstance(n, dict) and n.get("name")}
bytype = {n.get("id"): n.get("type") for n in nodes if isinstance(n, dict)}

if not nodes: err.append("no nodes")
for dup in {i for i in ids if ids.count(i) > 1}: err.append(f"duplicate node id: {dup}")
for n in nodes:
    if not isinstance(n, dict): continue
    for f in ("name", "type"):
        if not n.get(f): err.append(f"node {n.get('id') or '?'}: missing {f}")

starts = [n for n in nodes if n.get("type") == "start"]
if len(starts) != 1: err.append(f"expected exactly 1 start node, found {len(starts)}")

# Endpoints must be <node_id>.<handle> and the id must exist.
seen, srcs = set(), []
for c in conns:
    for end in ("source", "target"):
        v = c.get(end, "")
        if "." not in v:
            err.append(f"connection {end} '{v}' is not <node_id>.<handle>"); continue
        nid, handle = v.split(".", 1)
        if nid not in ids: err.append(f"connection {end} '{v}' points at unknown node '{nid}'")
        else:
            seen.add(nid)
            if end == "source": srcs.append((nid, handle))
        if end == "target" and handle != "Input":
            warn.append(f"target handle '{handle}' is unusual; it is normally 'Input'")

# Orphans are excluded from the saved version.
for o in [i for i in ids if i not in seen]:
    err.append(f"node '{o}' is referenced by no connection and will NOT be saved")

# Variables use node NAME, not node id.
for m in re.finditer(r"\{\{\s*([A-Za-z0-9_-]+)\s*\.", json.dumps(d)):
    ref = m.group(1)
    if ref in ids and ref not in names:
        err.append(f"variable '{{{{{ref}...}}}}' uses a node id; variables use the node NAME")

# ---- source handles. Nothing server-side validates these: a typo saves with
# 201 and becomes a permanently dead branch. Needs the catalogue, so it only
# runs when credentials are present.
A, T = os.environ.get("PLIVO_AUTH_ID"), os.environ.get("PLIVO_AUTH_TOKEN")
BASE = os.environ.get("PLIVO_API_BASE", "https://api.plivo.com")
if not (A and T):
    note.append("handle check SKIPPED (set PLIVO_AUTH_ID/PLIVO_AUTH_TOKEN to enable) "
                "-- a wrong source handle is not caught anywhere else")
else:
    try:
        req = urllib.request.Request(f"{BASE}/v1/Account/{A}/AgentFlowNode/")
        req.add_header("Authorization", "Basic " +
                       base64.b64encode(f"{A}:{T}".encode()).decode())
        cat = json.load(urllib.request.urlopen(req, timeout=20))
        static = {o["node_type"]: {s["id"] for s in (o.get("output_states") or [])}
                  for o in cat["objects"]}
        for nid, handle in srcs:
            ntype = bytype.get(nid)
            if ntype not in static:
                warn.append(f"node '{nid}': unknown type '{ntype}', cannot check handles")
                continue
            allowed = set(static[ntype])
            # Some nodes mint handles from their own config -- branch names one
            # per condition alias -- so those are legal despite not being in
            # output_states.
            model = ((next(n for n in nodes if n.get("id") == nid).get("config") or {})
                     .get("model") or {})
            if isinstance(model, dict):
                for cond in (model.get("conditions") or []):
                    if isinstance(cond, dict) and cond.get("alias"):
                        allowed.add(cond["alias"])
            if handle not in allowed:
                err.append(f"'{nid}.{handle}' is not a valid handle for type '{ntype}'. "
                           f"Valid: {', '.join(sorted(allowed)) or '(none)'}")
    except Exception as e:
        note.append(f"handle check could not run ({e}); source handles NOT verified")

for e in err:  print("ERROR  ", e)
for w in warn: print("warn   ", w)
for n in note: print("note   ", n)
print(f"\n{len(err)} error(s), {len(warn)} warning(s)")
sys.exit(1 if err else 0)
PY
```

Exit code is non-zero if anything must be fixed. Fix errors before POSTing; warnings are cosmetic but worth doing.

## When it goes wrong

| Response | Cause | Fix |
|---|---|---|
| `422` "not referenced by any connection" | Rule 2 — orphan node | Wire it, or drop it from `nodes` |
| `400` on a connection | Rule 4 — endpoint is not `<id>.<handle>` | Add the handle; targets take `.Input` |
| `400` "Payload is not a valid JSON" | Body is not a JSON **object** | Send an object, not an array or string |
| `401` | Path `auth_id` ≠ authenticated `auth_id` | Make them the same value |
| `422` on Publish | Agent is already `ACTIVE` | Check `state` first; `Pause` then `Resume` to cycle |
| wrong page size | `limit` > 20 | NOT an error — silently clamped to 20. Read `meta.limit`, and page with `offset` |
| Saves fine, does nothing at runtime | Rule 3 — variable used a node id, or an invented trigger field | Switch it to the node `name`; verify the path against an example or a real run |
| **One branch never fires; no error anywhere** | Rule 5 — source handle is misspelled or invented. **`201`, `GET` and `Publish` all report success** | Run the preflight with credentials so handles are checked against the catalogue |
| Update returns `202` but nothing changed | You changed a node `id` | Keep ids stable; post the full graph |

## Updating an agent

POST the **whole** graph, not a patch. The safe pattern is read-modify-write:

```bash
curl -s -u "$A:$T" "$B/AgentFlow/AGENT_ID/" > flow.json
# edit flow.json — keep every node id exactly as it came back
python3 -c "
import json; d=json.load(open('flow.json'))
json.dump({'nodes':d['nodes'],'connections':d['connections']}, open('body.json','w'))"
curl -s -X POST -u "$A:$T" -H "Content-Type: application/json" \
  --data-binary @body.json "$B/AgentFlow/AGENT_ID/"
```

`name` and `description` can be sent on their own, but a graph edit must include the full `nodes` + `connections`.

## Sanity check

If all of these hold, the flow is very likely correct:

- The preflight script exits `0` **and did not print "handle check SKIPPED"**. A skipped handle check means the riskiest mistake went unverified.
- `GET /AgentFlow/{agent_uuid}/` returns the same node **and connection** counts you sent.
- Every `{{...}}` reference matches a node `name` in the same flow, and every path after that name came from an example or a real run rather than from you.
- The Start node's `config.model.triggers` contains the handle its outgoing connection uses.
- Each branching node has one connection per outcome you care about, and every one of those handles appeared in the preflight's "Valid:" list.
- After `Publish`, `state` is `ACTIVE`.

Note: `GET /AgentFlowNode/{type}/` also returns `x-plivo-coverage`, listing validation rules that are dropped or degraded for that node type. Worth reading when a node's config is accepted but behaves oddly — it tells you which checks are not being applied.
