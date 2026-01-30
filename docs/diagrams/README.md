# Architecture Diagrams

This directory contains Mermaid diagram sources for the SRE Observability Platform.

## Viewing Diagrams

These `.mmd` files render automatically on GitHub. You can also:

- **GitHub**: Just click the file — GitHub renders Mermaid natively
- **VS Code**: Install the [Mermaid Preview](https://marketplace.visualstudio.com/items?itemName=bierner.markdown-mermaid) extension
- **CLI**: Use [mmdc](https://github.com/mermaid-js/mermaid-cli) to export to PNG/SVG:
  ```bash
  npx -p @mermaid-js/mermaid-cli mmdc -i observability-stack.mmd -o observability-stack.png
  ```

## Diagrams

| File | Description |
|------|-------------|
| `observability-stack.mmd` | End-to-end observability data flow |
| `alert-routing.mmd` | Alertmanager routing tree and receivers |
