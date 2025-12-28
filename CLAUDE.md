# CLAUDE.md

This file provides guidance for AI coding agents working in this repository.

## Project Purpose

This repository is a sandbox for AI coding agents to work autonomously on experiments, exercises, and projects. Agents have complete freedom to explore, create, and iterate without requiring constant human oversight. The goal is to enable asynchronous coding workflows where agents can be given large tasks and complete them independently.

## Repository Structure

This is a monolithic repository. Each experiment/exercise/project should be self-contained in its own subdirectory at the root level:

```
ai-coding-experiments/
├── CLAUDE.md
├── README.md
├── project-a/
│   ├── CLAUDE.md
│   ├── README.md
│   └── ...
├── project-b/
│   ├── CLAUDE.md
│   ├── README.md
│   └── ...
└── ...
```

## Guidelines

### Creating New Projects

1. Create a new directory at the repository root with a descriptive name
2. Include a `CLAUDE.md` in the project directory with project-specific guidance:
   - Build and test commands
   - Project-specific conventions or patterns
   - Any context future agents need to work effectively on the project
3. Include a `README.md` in the project directory explaining:
   - What the project is
   - How to run/use it
   - Any dependencies or setup required
4. Update the top-level `README.md` to list the new project with a brief description

### Working Autonomously

- You have free rein to make decisions about implementation details
- Create whatever files, directories, and code are needed to complete the task
- Use appropriate languages, frameworks, and tools for the problem at hand
- Document your work so others (human or AI) can understand and build on it

### Code Quality

- Write clean, readable code
- Include comments where helpful
- Handle errors appropriately
- Test your code when practical
