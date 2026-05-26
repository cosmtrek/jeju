# DeepSeek

DeepSeek uses an OpenAI-compatible Chat Completions API.

For Jeju, use:

```yaml
models:
  default: primary
  providers:
    primary:
      provider: deepseek
      model: deepseek-v4-flash
      env_key: DEEPSEEK_API_KEY
```

`provider: deepseek` defaults `base_url` to `https://api.deepseek.com` and sends requests to `/chat/completions`. `env_key` is the name of the environment variable that contains the API key.

The DeepSeek provider enables JSON response mode by default because Jeju V0 expects model output to be a JSON action.

Run the local DeepSeek fixture:

```bash
export DEEPSEEK_API_KEY=sk-...
make test-agent-deepseek
```

Use a custom environment variable name:

```bash
export MY_DEEPSEEK_KEY=sk-...
JEJU_DEEPSEEK_ENV_KEY=MY_DEEPSEEK_KEY make test-agent-deepseek
```
