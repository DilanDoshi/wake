export type Options = {
  endpoint: string
  key: string
  json: boolean
}

const defaults: Options = {
  endpoint: "https://api.harbor.dev",
  key: "",
  json: false,
}

export function parse(argv: string[]): Options {
  const o = { ...defaults }
  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === "--endpoint") o.endpoint = argv[++i]
    else if (argv[i] === "--key") o.key = argv[++i]
    else if (argv[i] === "--json") o.json = true
  }
  return o
}
