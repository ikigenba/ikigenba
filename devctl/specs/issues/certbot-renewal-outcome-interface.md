# Certbot renewal outcome has no verified interface

## Filing context

The D06 build run attempted to implement `space start` and halted at
R-91CM-M2ZV.

## Friction

R-91CM-M2ZV requires exactly one successful `Host.Sudo` call with exactly the
arguments `certbot` and `renew`. It must distinguish an actual renewal from a
successful no-op, and the distinction must come from a verified Certbot
interface rather than successful exit alone.

D06 does not declare a configured Certbot deploy/post hook, a marker emitted by
such a hook, or any other machine-readable result contract. No implementation
in this project establishes one. The required distinction therefore cannot be
made from the declared command and returned `host.Output` without parsing
Certbot's human-facing console output.

## Evidence

Certbot's official renewal documentation says that `certbot renew` exits zero
both when renewal succeeds and when no certificate needs renewal, and says to
use `--deploy-hook` when a command must run only after an actual renewal:

<https://eff-certbot.readthedocs.io/en/latest/using.html#renewing-certificates>

Certbot's official compatibility policy says that no aspect of its console or
log output is stable and that it may change at any time:

<https://eff-certbot.readthedocs.io/en/stable/compatibility.html>

Consequently, exit status does not prove either required outcome and parsing
stdout or stderr would not use a verified interface. Adding a deploy-hook
argument in devctl would violate the required exact argument vector. Assuming a
preconfigured hook and private output marker would invent an undeclared host
contract.

## Suggested resolution

Declare a stable, machine-readable renewal-result interface. For example,
require host setup to install a Certbot deploy hook that emits a specified
marker and define how `space start` distinguishes that marker from a successful
no-op. Alternatively, permit `space start` to supply a specified deploy hook in
the renewal command. Then replace R-91CM-M2ZV with a newly minted requirement
that names the complete interface.
