# JIRA vars

This little project aims at exporting some metrics out of your JIRA projects.
The initial motiviation was to export the number of tickets in the backlog and
the current sprint progress into a format that we could (1) display on our
Grafana board and (2) export to Prometheus for archival and potentially also
alerting.

This implementation pretty much does just that. You create a configuration file
where you specify what JQL queries should be run and the number of matching
issues is exported as a Gauge to Prometheus.

## Configuration

The configuration is rather minimal: All you need are some JIRA credentials and
the metrics that you want to collect. The JIRA password can optonally be passed 
via environment variable `JIRA_PASSWORD`.

Sample configuration:

```
baseURL: https://jira.company.net
login: "{{ vault "secret/accounts/work/me" "login"}}"
password: "{{ vault "secret/accounts/work/me" "password"}}"
metrics:
    - name: taa_backlog_size
      help: Number of items inside our backlog
      interval: 2m
      jql: |
          project in (project1, project2)
          AND type not in (tempo)
          AND (sprint IS EMPTY OR sprint not in openSprints())
          AND status in (open, "Ready for Development", "Ready for Refinement", Reopened)

```

## Usage

```
Usage of ./jiravars:
      --config string      Path to a configuration file
      --http-addr string   Address the HTTP server should be listening on
                           (default "127.0.0.1:9300")
      --verbose            Verbose logging
```

If you want to use something like [tpl][] to make your configuration a bit more dynamic,
you can set `--config -` to make jiravars read its configuration from stdin.

[tpl]: https://github.com/zerok/tpl


## Custom http headers

It is possible to add custom http headers to be send with each Jira request. 
Therefore the config can contain a httpHeader section:

```
httpHeaders:
  X-Custom-Header: custom-value
```
