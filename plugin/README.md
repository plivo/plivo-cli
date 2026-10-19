# Plivo plugin

Plivo skills for coding agents: put voice agents on real phone calls and run Plivo tasks from your agent.

- **plivo-cli**: run Plivo tasks with the Plivo CLI
- **plivo-sip-trunking**: connect LiveKit, ElevenLabs, Retell, Vapi, xAI or a self-hosted SIP stack to phone calls over Plivo SIP trunking
- **plivo-audio-streaming**: a voice agent on real calls through audio streaming
- **plivo-voice-xml**: write, check and test Plivo Voice XML

Install in Claude Code:

```
/plugin install plivo --marketplace plivo/plivo-cli
```

The skills drive the [Plivo CLI](https://github.com/plivo/plivo-cli#install). Docs: https://www.plivo.com/docs/

The skills here are copies of the ones in the repository's `*-skill/` folders; `make plugin-skills` refreshes them.
