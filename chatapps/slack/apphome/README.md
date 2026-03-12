# Slack App Home: Capability Center

The `apphome` package implements the **Capability Center**—a form-based interface within the Slack App Home tab that allows users to trigger predefined, complex AI tasks with structured inputs.

## 🚀 Overview

Instead of typing long prompts manually, users can go to the **Home** tab of the HotPlex Slack app to find a catalog of "Capabilities". Each capability presents a form (modal) to collect necessary parameters, which are then used to generate a high-quality prompt for the HotPlex engine.

## ⚙️ Configuration: `capabilities.yaml`

Capabilities are defined declaratively in `capabilities.yaml`. This allows for adding new AI tools without changing Go code.

### Example Entry:
```yaml
- id: code_review
  name: 代码审查
  icon: ":mag:"
  description: 对指定文件进行安全/性能/风格审查
  category: code
  parameters:
    - id: target
      label: 审查目标
      type: text
      required: true
    - id: focus
      label: 审查重点
      type: select
      options: [all, security, performance]
  prompt_template: |
    请对以下内容进行代码审查:
    目标: {{.target}}
    重点关注: {{.focus}}
```

### Parameter Types:
- `text`: Single-line input.
- `multiline`: Large text area for code or long descriptions.
- `select`: Dropdown with predefined options.

## 🏗️ Core Components

- **[registry.go](registry.go)**: Loads and maintains the list of enabled capabilities from `capabilities.yaml`.
- **[form.go](form.go)**: Translates capability definitions into Slack **Block Kit Modals** for user input.
- **[executor.go](executor.go)**: Handles form submissions, resolves templates, and injects tasks into the HotPlex engine.
- **[handler.go](handler.go)**: Manages the App Home lifecycle, including rendering the initial catalog view.

## 🛠️ Usage & Extension

### Adding a New Capability
1. Open `capabilities.yaml`.
2. Add a new item following the existing schema.
3. Restart HotPlex (or trigger a config reload).
4. The new capability will appear instantly in the Slack App Home tab.

### UX Best Practices
- Use clear **Icons** (`:bug:`, `:memo:`) to make categories scannable.
- Provide descriptive **Placeholders** in forms.
- Use `preferred_model` in `brain_opts` to ensure the task uses the most capable model (e.g., Claude 3.5 Sonnet).
