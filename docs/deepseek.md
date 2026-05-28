# DeepSeek

DeepSeek uses an OpenAI-compatible Chat Completions API.

For Jeju, use:

```yaml
models:
  providers:
    primary:
      type: openaiCompatible
      preset: deepseek
      model: deepseek-v4-flash
      envKey: DEEPSEEK_API_KEY
      thinking:
        type: disabled
```

`preset: deepseek` defaults `baseUrl` to `https://api.deepseek.com` and sends requests to `/chat/completions`. `envKey` is the name of the environment variable that contains the API key.

The DeepSeek preset enables JSON response mode by default for non-native fallback output. It also defaults `thinking.type` to `disabled`. If you set `thinking.type: enabled`, Jeju records the returned `reasoning_content`, shows a short console preview, and replays it on subsequent tool-call turns as required by DeepSeek thinking mode.

Run the shared local fixture with DeepSeek:

```bash
export DEEPSEEK_API_KEY=sk-...
make test-agent PROVIDER=deepseek
```

Use a custom environment variable name:

```bash
export MY_DEEPSEEK_KEY=sk-...
JEJU_DEEPSEEK_ENV_KEY=MY_DEEPSEEK_KEY make test-agent PROVIDER=deepseek
```

The shared local fixture also supports MiMo:

```bash
export MIMO_API_KEY=sk-...
make test-agent PROVIDER=mimo
```

Set `JEJU_MIMO_BASE_URL` to override the default MiMo endpoint.
