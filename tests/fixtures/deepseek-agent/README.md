# DeepSeek Fixture Custom Tool

This fixture includes `keyword_count` as an example custom command tool.

The tool itself is a normal CLI script:

```bash
./tools/keyword_count.py --text "Jeju uses tools" --keyword Jeju
```

The manifest keeps Jeju-specific adaptation in the tool declaration:

- `schema` tells the model what JSON input to produce.
- `args` maps that JSON input to the CLI arguments.
