# D06-routing

Every request to a sandbox arrives at the sandbox's own nginx, which `up` configures and starts as a user unit (D04 writes both files and starts or reloads nginx; this document owns only what the two files say). nginx listens on `127.0.0.1:<port>` in plain HTTP and routes by host name, one name per app, to each app's Unix socket. Every sandbox holds an `auth` app, since `up` refuses a checkout without one (D04), and every request for another app is first put to auth's `/check` as an internal subrequest, and auth's answer decides it.

The configuration is one self-contained file: nothing is included from the system's nginx, and every runtime path nginx would otherwise take from its compiled-in defaults under `/var` is named under `<data>/nginx`, D03's nginx directory (R-F9C7-K7Y5), instead (pid file, the five temporary directories), with the error log on stderr and no access log, since nginx would otherwise open `/var/log/nginx/access.log` and fail as an ordinary user. The file sets neither `daemon` nor `user`; how nginx runs is the unit's business. Paths are written nginx-quoted, so a home directory holding spaces, `%` or `$` reads back unchanged; D03 keeps quotes and backslashes out of the state root, so none needs escaping. Every block holds exactly the directives this document lists and nothing more, because nginx inherits a directive written at `http` or `server` level into every location below it: a stray `add_header` there would reach the sign-in redirect, a stray `auth_request` the named locations, and `proxy_intercept_errors` would turn an app's own 401 into a sign-in. Server names of up to 137 characters (a 63-character app in a 63-character sandbox under `.localhost`) need a larger server-name hash bucket than nginx's default.

Every server listens on `127.0.0.1:<port>` and nowhere else. One default server answers 404 to any host the file does not name: an unknown name under the sandbox, a foreign host, and the bare sandbox name when no app is the default. Bare `localhost` is the address Google returns the browser to, and its server sends every path on to the same path and query at auth's origin. Each app has one server named for it, and the default app's server also carries the bare sandbox name, so the default answers there exactly as at its own name. Each app's socket is an upstream named `app_<app>`.

Every location that passes a request to an app sets the same forwarding headers: `Host` as the client sent it, port included; the client's address in `X-Real-IP` and in `X-Forwarded-For`, which nginx replaces rather than appends to, because nginx is the first proxy and a client's own value is forged; `X-Forwarded-Proto: http`; and `X-Request-Id` from nginx's own `$request_id`, replacing a client's. The identity headers, `X-User-Id` and `X-User-Email`, are set in every such location, to the empty value (which nginx does not send) wherever auth has not just answered, so a client's own never passes, on any host.

Every app other than `auth` is gated. auth's own server is never gated, and answers `/check` itself with 404. A gated server puts each request to an internal location at the reserved path `/_sandbox/auth`, which proxies to auth's `/check` with the request's headers (so its `Cookie` and `Authorization`), no body, the same forwarding headers and request id, and no identity headers; the reserved path is the only one the gate takes from an app, and a client asking for it gets 404. A 2xx lets the request through with auth's `X-User-Id` and `X-User-Email`; a 403 is answered 403; a 401 is answered by a named location, 302 to sign in at auth's origin with the requested URL in `return` everywhere except `/mcp` and below, where it is nginx's own bearer challenge. nginx's auth_request copies any `WWW-Authenticate` that auth's 401 carried onto the response, and `proxy_hide_header` does not stop it; passing the 401 through a named location with `satisfy any` and `allow all` drops it (nginx clears pending `WWW-Authenticate` headers whenever an access check succeeds under `satisfy any`), and `try_files` then hands the request to the location that answers. An app's own 401 is not nginx's and passes through untouched.

Apps bring no nginx configuration of their own. On a host, production's nginx includes each app's `/opt/<app>/etc/nginx.conf*` in that app's server blocks; the sandbox does not support such per-app fragments, and its configuration is entirely what this document lists. An app's `etc/nginx.conf`, or any other entry of its `etc/` whose name begins `nginx.conf`, is neither included nor read, so it can neither change the sandbox's routing nor make `up` fail. An app that relies on a fragment behaves differently in the sandbox than deployed; that is a known gap, chosen so the sandbox's routing stays the fixed, testable shape below.

The nginx unit (D03's R-FKBB-05ME) is a user unit, described as `sandbox <name>: nginx`, so systemd's user manager runs nginx as the developer; it sets no user, group or privilege of its own, and it has no `[Install]` section because sandbox never enables a unit. Its command is the bare name `nginx`, which systemd resolves with its own compiled-in search path (the standard `bin` and `sbin` directories), not the developer's `PATH`, so sandbox looks nothing up. nginx is told its prefix (`<data>/nginx`) and its configuration file on the command line, and `-e stderr` replaces the error log nginx would otherwise open at start-up from its compiled-in path, so nothing is written under `/var/log`; the error log of the running nginx is stderr as the configuration says. nginx keeps the stderr the user manager gave it when it forks into the background, and its workers inherit it, so what the master and every worker log lands in the unit's journal for `logs`.

The unit is `Type=forking`, its pid file D03's nginx pid file, the path the configuration's `pid` names. nginx binds its listening socket before it forks into the background and exits non-zero when the bind fails, so `systemctl --user start` succeeds only once nginx is listening and fails when the port is already held. Reload runs two commands in order: nginx's configuration test, then nginx's own reload signal. A configuration that fails the test fails `systemctl --user reload` and leaves the running nginx serving the configuration it had; one that passes makes the running nginx load the newly written file. nginx applies a reload asynchronously: the command returns once the signal is sent, and the new configuration answers a moment later. Stopping the unit uses systemd's default, terminating every process of the unit, after which nginx has removed its pid file. Paths come from the developer's `HOME` or XDG variables and may hold spaces, `%` or `$`, so the unit writes each one in the form D03 gives its position, which systemd reads back unchanged: the two paths on the command lines unit-quoted as arguments, the pid file as a unit path value.

## REQUIREMENTS

In these requirements, the configuration is the nginx configuration file `up` writes (D03's R-FAK3-XZOU); `<data>`, `<name>` and `<port>` are the sandbox's data directory (D03's R-F38P-ND8O), name and port; the apps are the apps `up` discovered, the default app the one among them whose `default` is true. Directives and their arguments are read as R-SWXP-LN7R says; whitespace, comments, the quoting and escaping of an argument (except where R-XKZH-R9GD fixes it) and the order of directives within a block are not fixed, since nginx gives none of the directives used here a meaning by order.

### File and runtime paths

- R-SWXP-LN7R: Every directive, block and argument this document fixes MUST be found in the configuration as nginx's parser reads the file: outside quotes, a `#` that starts a word begins a comment running to the end of the line; a word not starting with a quote ends at a space, tab, carriage return, newline, `;` or `{`, except a `{` directly after `$`; a word starting with `"` or `'` ends at the next same quote, which is not part of it and must be followed by a space, tab, carriage return, newline, `;` or `{`, so `""` is an empty argument; a `\` keeps the character after it from ending a word or a quoted word; within any word, `\"`, `\'` and `\\` stand for their second character, `\t`, `\r` and `\n` for a tab, carriage return and newline, and a `\` before any other character stands for itself; `;` ends a directive, `{` after a directive's words opens its block, and a `}` that starts a word closes a block.

- R-4O92-QDQB: The configuration's main context MUST hold exactly one `pid` directive, one `error_log` directive, one `events` block and one `http` block, and no other directive, so it holds no `daemon`, `user`, `include` or `load_module`.

- R-XKZH-R9GD: Every argument of the configuration that holds a path under `<data>` MUST be written nginx-quoted: the path enclosed in `"` with no character of it changed, verified at least with a state root `/tmp/a b%c$d` by the `pid` directive of sandbox `wip` having the argument `"/tmp/a b%c$d/ikigenba/sandbox/wip/nginx/nginx.pid"`; every such path lies under the state root, which D03's root character check keeps free of `"`, `'` and `\`.

- R-SY5L-ZEYG: The configuration's `events` block MUST hold no directive.

- R-G47E-M7ZV: The configuration's `http` block MUST hold exactly one each of `access_log`, `client_body_temp_path`, `proxy_temp_path`, `fastcgi_temp_path`, `uwsgi_temp_path`, `scgi_temp_path` and `server_names_hash_bucket_size`, the `upstream` blocks of R-4WSD-ERX6, and the `server` blocks of the default server, of the bare-localhost server and of the apps' servers, and no other directive or block.

- R-YRNZ-58EO: The configuration's `pid` directive MUST have a single argument, the sandbox's nginx pid file (D03).

- R-4QOV-HX7P: The configuration's `error_log` directive MUST have the single argument `stderr`, and the configuration MUST hold no other `error_log` directive.

- R-4T4O-9GP3: The configuration's `http` block MUST hold `access_log off`, and the configuration MUST hold no other `access_log` directive.

- R-4UCK-N8FS: The configuration's `http` block MUST hold `client_body_temp_path <data>/nginx/client_body`, `proxy_temp_path <data>/nginx/proxy`, `fastcgi_temp_path <data>/nginx/fastcgi`, `uwsgi_temp_path <data>/nginx/uwsgi` and `scgi_temp_path <data>/nginx/scgi`, each with that single argument.

- R-4VKH-106H: The configuration's `http` block MUST hold `server_names_hash_bucket_size 256`.

### Servers and upstreams

- R-4WSD-ERX6: For each app, the configuration's `http` block MUST hold one `upstream` block named `app_<app>` whose only directive is `server` with the single argument `unix:` followed by that app's socket path (D03's R-FLJ7-DXD3), and the configuration MUST hold no other `upstream` block.

- R-4Y09-SJNV: Every `server` block of the configuration MUST hold exactly one `listen` directive, whose first argument is `127.0.0.1:<port>`, and the configuration MUST hold no `listen` directive outside a `server` block, so nginx accepts connections on no other address.

- R-T0LE-QYFU: Exactly one `server` block of the configuration, the default server, MUST hold exactly two directives: `listen` with exactly the arguments `127.0.0.1:<port>` and `default_server`, and `return` with exactly the argument `404`.

- R-T1TB-4Q6J: Every `listen` directive of the configuration other than the default server's MUST have exactly one argument, `127.0.0.1:<port>`.

- R-G5FA-ZZQK: The configuration MUST hold one `server` block, the bare-localhost server (the server for D03's callback origin, R-0T3R-7GO5), whose directives are exactly its `listen`, `server_name localhost`, and `return` with exactly the arguments `302` and auth's origin (D03's R-0QNY-FX6R for the app `auth`) followed by `$request_uri`.

- R-52VV-BMMN: For each app, the configuration MUST hold exactly one `server` block, that app's server, whose `server_name` names `<app>.<name>.localhost`; its `server_name` MUST also name `<name>.localhost` when the app is the default app, and MUST name nothing else.

- R-T5H0-A1EM: When no app is the default app, the configuration MUST NOT hold a `server_name` directive naming `<name>.localhost`.

- R-G6N7-DRH9: auth's server MUST be ungated, and every other app's server MUST be gated.

### Forwarded headers

- R-56JK-GXUQ: Every `location` block of the configuration that holds `proxy_pass` MUST hold `proxy_set_header Host $http_host`, `proxy_set_header X-Real-IP $remote_addr`, `proxy_set_header X-Forwarded-For $remote_addr`, `proxy_set_header X-Forwarded-Proto http` and `proxy_set_header X-Request-Id $request_id` (the forwarding headers), and exactly one `proxy_set_header` each for `X-User-Id` and `X-User-Email`.

- R-T94P-FCMP: In the auth check location and in every ungated app location, the `proxy_set_header` directive for each identity header (`X-User-Id` and `X-User-Email`) MUST have the empty string as its value, so nginx sends neither header.

### Ungated apps

- R-TACL-T4DE: An ungated app's server MUST hold `location /` (the ungated app location) whose directives are exactly `proxy_pass http://app_<app>`, the forwarding headers, and the `proxy_set_header` directives for the identity headers of R-T94P-FCMP.

- R-TBKI-6W43: An ungated app's server MUST hold exactly one `listen`, one `server_name`, the ungated app location and, when the app is `auth`, its `location = /check`, and no other directive or block.

- R-5A79-M92T: auth's server MUST also hold `location = /check` whose only directive is `return 404`.

### App fragments

- R-KU5J-85KD: An app's `etc/nginx.conf`, and any other entry of its `etc/` whose name begins `nginx.conf`, MUST NOT affect `up`: `up` MUST NOT fail because of them, and the configuration it writes MUST be byte-identical to the one the same `up` writes from the same checkout without them, verified at least by an `up` that succeeds with `dummy/etc/nginx.conf` holding `location /x { return 418; }`, `dummy/etc/nginx.conf.bad` a regular file of mode 000 holding `server {`, and `dummy/etc/nginx.conf.d` a directory of mode 000, writing the same configuration, byte for byte, as an `up` of that checkout without those three entries.

### Gated apps

- R-TCSE-KNUS: A gated server MUST hold `location = /_sandbox/auth` (the auth check location) whose directives are exactly `internal`, `proxy_pass http://app_auth/check`, `proxy_pass_request_body off`, `proxy_set_header Content-Length` with the empty string as its value, the forwarding headers, and the `proxy_set_header` directives for the identity headers of R-T94P-FCMP.

- R-TE0A-YFLH: A gated server MUST hold exactly one `listen`, one `server_name`, the auth check location, the gated locations, `location @sandbox_signin`, `location @sandbox_bearer`, `location @sandbox_signin_reply` and `location @sandbox_bearer_reply`, and no other directive or block.

- R-5DUY-RKAW: A gated server MUST hold `location /`, `location = /mcp` and `location /mcp/` (the gated locations), each holding exactly `auth_request /_sandbox/auth`, `auth_request_set $sandbox_user_id $upstream_http_x_user_id`, `auth_request_set $sandbox_user_email $upstream_http_x_user_email`, one `error_page` directive, `proxy_pass http://app_<app>`, the forwarding headers, `proxy_set_header X-User-Id $sandbox_user_id` and `proxy_set_header X-User-Email $sandbox_user_email`.

- R-5F2V-5C1L: The `error_page` directive of a gated server's `location /` MUST be `error_page 401 = @sandbox_signin`, and that of its `location = /mcp` and its `location /mcp/` MUST be `error_page 401 = @sandbox_bearer`.

- R-5GAR-J3SA: A gated server MUST hold `location @sandbox_signin` whose directives are exactly `satisfy any`, `allow all` and `try_files /.sandbox-none @sandbox_signin_reply`, and `location @sandbox_bearer` whose directives are exactly `satisfy any`, `allow all` and `try_files /.sandbox-none @sandbox_bearer_reply`.

- R-TF87-C7C6: A gated server MUST hold `location @sandbox_signin_reply` whose only directive is `return` with exactly the arguments `302` and auth's origin (D03's R-0QNY-FX6R for the app `auth`) followed by `/?return=$scheme://$http_host$request_uri`.

- R-5IQK-AN9O: A gated server MUST hold `location @sandbox_bearer_reply` whose directives are exactly `default_type text/plain`, `add_header WWW-Authenticate` with the value `Bearer realm="ikigenba"` and the third argument `always`, and `return 401` whose second argument is `authentication required: send Authorization: Bearer <token>` followed by a newline.

### nginx unit

- R-5DLC-QFHT: The nginx unit file (D03's R-FKBB-05ME) MUST hold exactly two sections, `[Unit]`, whose only setting is `Description=sandbox <name>: nginx`, and `[Service]`, so it has no `[Install]` section.

- R-ZUPX-F7J2: The `[Service]` section of the nginx unit MUST hold no settings other than one `Type=`, one `PIDFile=`, one `ExecStart=` and two `ExecReload=`, so it sets no `User=`, `Group=` or other privilege setting.

- R-ZVXT-SZ9R: The nginx unit MUST set `Type=forking`.

- R-XPV3-ACF5: The nginx unit MUST set `PIDFile=` to the sandbox's nginx pid file (D03) written as a unit path value (D03 R-XJRL-DHPO).

- R-XON6-WKOG: The nginx unit's `ExecStart=` MUST be the bare word `nginx`, with no path and no special executable prefix, followed by exactly six arguments: the bare words `-p`, `-c`, `-e` and `stderr`, and the nginx directory (D03's R-F9C7-K7Y5) and the nginx configuration file (D03's R-FAK3-XZOU) unit-quoted as arguments (D03's R-XG3W-86HL), as the pairs `-p` nginx directory, `-c` nginx configuration file and `-e` `stderr`, each flag directly followed by its value, the three pairs in any order.

- R-ZZLI-YAHU: The nginx unit's two `ExecReload=` settings MUST be, in this order, the `ExecStart=` command line with the argument `-t` added after it, and the `ExecStart=` command line with the arguments `-s` and `reload` added after it.
