---
name: plivo-cx-agents
description: Build, update and publish Plivo CX Agents (conversation flows made of nodes) through the public Agents API. Trigger whenever the user wants to create, edit, wire, validate or publish a Plivo CX agent or flow, asks which Plivo nodes to use, or mentions AgentNode / agent_id / the Agents API.
---

# Plivo CX Agents skill

A Plivo CX **Agent** is a directed graph: `nodes` (what happens) plus `connections` (what happens next). You build one by POSTing that graph as JSON. This file exists because the graph has a handful of non-obvious rules, and getting any one of them wrong produces an agent that returns `201 Created` and then does not work.

> Contract: `schema_version` from `GET /AgentNode/`. If the version there differs from what you assumed, re-read the node schema before building.

## If you are an AI agent — the rules that actually break flows

Read these six. Everything else you can discover from the API.

1. **Never invent node config.** `GET /AgentNode/{node_type}/` returns the node's JSON Schema *and a working example*. Read it and copy its shape. Guessing field names is the single most common failure.
2. **Every node must be reachable by a connection.** Membership in a flow version is derived from the connection graph, not from the `nodes` array. A node no connection points at is **not part of the flow**. The API rejects this with `422` and names the orphans.
3. **Connections use node `id`. Variables use node `name`.** These are different namespaces and mixing them is the most expensive mistake here, because it fails at *runtime*, not on save. See Rule 3 below.
4. **A connection endpoint is `<node_id>.<handle>`**, never a bare node id. Source handle comes from the source node's `output_states[].id`. Target handle is `Input`.
5. **Node `id`s are permanent.** Connections address nodes by id, so changing an id on update rewires or orphans the graph. Pick ids once and keep them.
6. **What you GET is what you can POST.** Read an agent, change one field, post the whole graph back. No reshaping needed.

Also: send `Content-Type: application/json`, keep the **trailing slash** on collection paths, and never exceed `limit=20` (it is rejected, not clamped).

## The workflow that works

```
1. GET /AgentNode/                     → which node types exist
2. GET /AgentNode/{type}/              → schema + working example, per node you need
3. build the graph                     → ids, names, positions, connections
4. run the preflight check (below)     → catch orphans + namespace errors offline
5. POST /Agent/                        → 201, returns agent_id
6. GET /Agent/{agent_id}/              → confirm nodes + connections came back
7. POST /Agent/{agent_id}/Publish      → DRAFT → ACTIVE
```

Do not skip step 4. It catches offline exactly the mistakes that otherwise cost you a round trip or ship broken.

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
| GET | `/Agent/` | list; `?limit=` ≤20, `?offset=`, `?state=DRAFT\|ACTIVE\|PAUSED` |
| POST | `/Agent/` | `201` → `{api_id, message:"created", agent_id, name}` |
| GET | `/Agent/{agent_id}/` | detail incl. `nodes` + `connections` |
| POST | `/Agent/{agent_id}/` | update; `202` → `{api_id, message:"changed"}` |
| DELETE | `/Agent/{agent_id}/` | `204` |
| POST | `/Agent/{agent_id}/Publish` | `DRAFT` → `ACTIVE`. **Empty body.** |
| POST | `/Agent/{agent_id}/Pause` | → `PAUSED`. Empty body. |
| POST | `/Agent/{agent_id}/Resume` | → `ACTIVE`. Empty body. |
| GET | `/Agent/{agent_id}/Run/` | run history |
| GET | `/Agent/{agent_id}/Run/{run_id}/` | one run + execution logs |
| GET | `/AgentNode/` | all node types |
| GET | `/AgentNode/{node_type}/` | schema + examples for one type |

The resource key is **`agent_id`** in every response, including list rows — not a bare `id`.

## Node shape

```json
{
  "id": "reply-1",
  "name": "Send Message",
  "type": "send_message",
  "left": 440, "top": 200,
  "config": { "model": { "...": "per-node, from GET /AgentNode/{type}/" } }
}
```

- `id` — yours to choose, referenced by connections, permanent.
- `name` — human label, **and the namespace variables use**.
- `type` — must be an exact `node_type` from the catalogue.
- `left` / `top` — canvas coordinates. Omit them and every node stacks at the same point, which is technically valid and visually unusable. Space them ~360px apart horizontally.
- `config.model` — a **flat dict** of the node's fields.

## Rule 3, expanded — the namespace trap

Connections address nodes by **id**. Template variables address them by **name**.

```json
{ "nodes": [
    { "id": "start-1", "name": "Start", "type": "start", ... },
    { "id": "reply-1", "name": "Send Message", "type": "send_message",
      "config": { "model": {
        "to": ["{{Start.message.from}}"]        ← name  ✅
      } } } ],
  "connections": [
    { "source": "start-1.message", "target": "reply-1.Input" }   ← ids  ✅
  ] }
```

```
❌ "to": ["{{start-1.message.from}}"]    id in a variable — saves fine, resolves to nothing at runtime
❌ "source": "Start.message"             name in a connection — 400 or a broken graph
```

If you rename a node, every variable referencing it must change too. This is a good reason to set `name` once and leave it.

## Handles

Source handle = an `id` from the source node's `output_states`, which `GET /AgentNode/{type}/` returns. Target handle is always `Input`.

| Node | Source handles (`output_states[].id`) |
|---|---|
| `start` | `message`, `call`, `chat`, `whatsapp`, `email`, `http`, `outbound_call`, `outbound_message`, `outbound_whatsapp`, `outbound_email`, `destination`, `call_hangup`, `incoming_slack` |
| `send_message` | `sent`, `failed` |
| `http_request` | `success`, `failed` |
| `ai_agent_v2` | `sent`, `error` |
| `queue_and_route` | `completed`, `timeout`, `enqueue_failed`, `assignment_failed`, plus DTMF digits |

Anything else: read `output_states` from the catalogue. Do not guess.

**The Start trigger you pick decides the agent's type.** `message` → an SMS-triggered agent; `http` → an API-triggered agent you can fire programmatically. The backend derives this for you; you only choose the handle.

To branch, give one node two outgoing connections on different handles:

```json
{ "source": "http-1.success", "target": "ok-1.Input" },
{ "source": "http-1.failed",  "target": "oops-1.Input" }
```

## Node catalogue at a glance

52 types. Use `GET /AgentNode/` for the live list; this is orientation only.

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

Two nodes, one connection. Verified end to end.

```bash
curl -s -u "$A:$T" -H "Content-Type: application/json" -d '{
  "name": "SMS auto-reply",
  "description": "replies to any inbound SMS",
  "nodes": [
    { "id": "start-1", "name": "Start", "type": "start",
      "left": 80, "top": 200,
      "config": { "model": { "triggers": ["message"] } } },
    { "id": "reply-1", "name": "Send Message", "type": "send_message",
      "left": 440, "top": 200,
      "config": { "model": {
        "from": "YOUR_PLIVO_NUMBER",
        "to": ["{{Start.message.from}}"],
        "text": "Thanks for your message, we will be right with you." } } }
  ],
  "connections": [
    { "source": "start-1.message", "target": "reply-1.Input" }
  ] }' "$B/Agent/"
```

Then publish it:

```bash
curl -s -X POST -u "$A:$T" "$B/Agent/AGENT_ID/Publish"
```

Note `config.model.triggers` on the Start node matches the handle used in the connection (`message`). Keep those two in agreement.

## Preflight check — run this before every POST

Catches the failures that a `201` will not tell you about. No dependencies.

```bash
python3 - flow.json <<'PY'
import json, re, sys
d = json.load(open(sys.argv[1]))
nodes, conns = d.get("nodes") or [], d.get("connections") or []
ids   = [n.get("id")   for n in nodes]
names = {n.get("name") for n in nodes if n.get("name")}
err, warn = [], []

if not nodes: err.append("no nodes")
for dup in {i for i in ids if ids.count(i) > 1}: err.append(f"duplicate node id: {dup}")
for n in nodes:
    for f in ("id", "name", "type"):
        if not n.get(f): err.append(f"node {n.get('id') or '?'}: missing {f}")
    if n.get("left") is None or n.get("top") is None:
        warn.append(f"node {n.get('id')}: no left/top, will stack on the canvas")

starts = [n for n in nodes if n.get("type") == "start"]
if len(starts) != 1: err.append(f"expected exactly 1 start node, found {len(starts)}")

# Rule 4 — endpoints must be <node_id>.<handle> and the id must exist
seen = set()
for c in conns:
    for end in ("source", "target"):
        v = c.get(end, "")
        if "." not in v:
            err.append(f"connection {end} '{v}' is not <node_id>.<handle>"); continue
        nid, handle = v.split(".", 1)
        if nid not in ids: err.append(f"connection {end} '{v}' points at unknown node '{nid}'")
        else: seen.add(nid)
        if end == "target" and handle != "Input":
            warn.append(f"target handle '{handle}' is unusual; it is normally 'Input'")

# Rule 2 — orphans are silently excluded from the version
for o in [i for i in ids if i not in seen]:
    err.append(f"node '{o}' is referenced by no connection and will NOT be saved")

# Rule 3 — the namespace trap: {{...}} must use node NAME, not node id
for m in re.finditer(r"\{\{\s*([A-Za-z0-9_-]+)\s*\.", json.dumps(d)):
    ref = m.group(1)
    if ref in ids and ref not in names:
        err.append(f"variable '{{{{{ref}...}}}}' uses a node id; variables use the node NAME")

for e in err:  print("ERROR  ", e)
for w in warn: print("warn   ", w)
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
| `400` on list | `limit` > 20 | Use ≤20 and page with `offset` |
| Saves fine, does nothing at runtime | Rule 3 — variable used a node id | Switch it to the node `name` |
| Update returns `202` but nothing changed | You changed a node `id` | Keep ids stable; post the full graph |

## Updating an agent

POST the **whole** graph, not a patch. The safe pattern is read-modify-write:

```bash
curl -s -u "$A:$T" "$B/Agent/AGENT_ID/" > flow.json
# edit flow.json — keep every node id exactly as it came back
python3 -c "
import json; d=json.load(open('flow.json'))
json.dump({'nodes':d['nodes'],'connections':d['connections']}, open('body.json','w'))"
curl -s -X POST -u "$A:$T" -H "Content-Type: application/json" \
  --data-binary @body.json "$B/Agent/AGENT_ID/"
```

`name` and `description` can be sent on their own, but a graph edit must include the full `nodes` + `connections`.

## Sanity check

If all of these hold, the flow is very likely correct:

- The preflight script exits `0`.
- `GET /Agent/{agent_id}/` returns the same node count you sent.
- Every `{{...}}` reference matches a node `name` in the same flow.
- The Start node's `config.model.triggers` contains the handle its outgoing connection uses.
- After `Publish`, `state` is `ACTIVE`.
