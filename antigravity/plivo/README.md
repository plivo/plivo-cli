# Plivo

Build Plivo CX agents, put your voice agent on real phone calls and run Plivo tasks from Antigravity. The skills drive the [Plivo CLI](https://github.com/plivo/plivo-cli) with your own Plivo account.

## What's included

- **plivo-cx-agents**: build, test and publish Plivo CX agents, AI conversation flows for calls and messages
- **plivo-cli**: run Plivo tasks with the Plivo CLI
- **plivo-sip-trunking**: connect LiveKit, ElevenLabs, Retell, Vapi, xAI or a self-hosted SIP stack to phone calls over Plivo SIP trunking
- **plivo-audio-streaming**: a voice agent on real calls through audio streaming
- **plivo-voice-xml**: write, check and test Plivo Voice XML

## Requirements

- A [Plivo](https://www.plivo.com) account.
- The Plivo CLI: `brew install plivo/tap/plivo`, or the [signed installer](https://github.com/plivo/plivo-cli#install). Sign in once with `plivo login` in your own terminal.

Install the plugin with the Antigravity CLI:

```
agy plugin install https://github.com/plivo/plivo-cli/tree/main/antigravity/plivo
```

## Data and services

The plugin ships only these five skill files: it runs no code of its own and stores nothing. The skills have your agent run the Plivo CLI on your machine, signed in with your own Plivo account. Data leaves your machine only to:

- **Plivo's API**, for what you ask: reading your numbers, applications, calls and messages, building, testing and publishing your CX agents, or sending a message, placing a call or changing a number after you confirm. Plivo keeps these records under its [privacy policy](https://www.plivo.com/legal/privacy/).
- **Plivo, as CLI usage data**: each CLI request carries the CLI version, OS and architecture and, when you are signed in, your account's email, auth ID, region and AOM UUID. `plivo config telemetry off` turns the account part off ([Privacy & telemetry](https://github.com/plivo/plivo-cli#privacy--telemetry)).
- **Services you pick during setup**, each one asked first:
  - plivo-audio-streaming: a tunnel to your local agent (Cloudflare's `cloudflared` or ngrok), and the AI providers in Pipecat's example agent (OpenAI, Deepgram, Cartesia) with your own keys. It clones the example from GitHub.
  - plivo-sip-trunking: the voice-agent platform you connect your Plivo number to (LiveKit, ElevenLabs, Retell, Vapi, xAI or your own SIP server), so calls flow there.

## Learn more

- [Plivo docs](https://www.plivo.com/docs/)
- [Support](https://help.plivo.com/)
- [Terms of service](https://www.plivo.com/legal/tos/)
- [Source and issues](https://github.com/plivo/plivo-cli)
