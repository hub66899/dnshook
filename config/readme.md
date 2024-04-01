## How to use

1. Define a config structure, like below:

```
type RestfulConf struct {
	Port         int
	LogMode      string        `json:",options=[file,console]"`
}
```

2. Write the yaml, toml or json config file:

- yaml example

```
# most fields are optional or have default values
port: 8080
logMode: console
```

