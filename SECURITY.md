# Security policy

Arv is pre-1.0. Security fixes go into the latest release and `main`.

## Reporting a vulnerability

Please report it privately through GitHub's private vulnerability reporting: the **Security** tab of this repository, then **Report a vulnerability**. Don't open a public issue for it.

Say what an Arv program, model reply, data source or log file can do that the runtime should have refused. Include a script that shows it, if you can. We aim to answer within 7 days.

## What is in scope

The capability boundary and the guarantees in [`docs/security-model.md`](docs/security-model.md):
- grants and their checks;
- the file and network checks;
- the model key;
- redaction;
- budgets;
- the effect log and replay.

The gaps listed there as known are not new findings unless you show more than the listed effect.

A host's own Go code is out of scope, including effects it registers. So is the OS kernel.
