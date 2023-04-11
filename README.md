<h1 align="center">
  <br>
  <a href="https://github.com/asabellico/kerbetor"><img src="static/kerbetor-logo.png" width="200px" alt="KerbeTOR"></a>
</h1>

<h4 align="center">Download manager for the dark web, using multiple TOR circuits.</h4>

<p align="center">
<img src="https://img.shields.io/github/go-mod/go-version/asabellico/kerbetor/initial-dev?filename=go.mod">
<a href="https://github.com/asabellico/kerbetor/releases"><img src="https://img.shields.io/github/downloads/asabellico/kerbetor/total"></a>
<a href="https://github.com/asabellico/kerbetor/graphs/contributors"><img src="https://img.shields.io/github/contributors-anon/asabellico/kerbetor"></a>
<a href="https://github.com/asabellico/kerbetor/releases/"><img src="https://img.shields.io/github/release/asabellico/kerbetor"></a>
<a href="https://github.com/asabellico/kerbetor/issues"><img src="https://img.shields.io/github/issues-raw/asabellico/kerbetor"></a>
<a href="https://github.com/asabellico/kerbetor/discussions"><img src="https://img.shields.io/github/discussions/asabellico/kerbetor"></a>
</p>

## Installation

```bash
go install github.com/asabellico/kerbetor@latest
```

and make sure TOR is installed in your system and exists in your PATH.

## Usage

```bash
kerbetor http://myonionsite.onion/file1
```

```bash
kerbetor http://myonionsite.onion/file1 --parallel-downloads 10 --tor-circuits 5
```

## Contributing

Pull requests are welcome. For major changes, please open an issue first
to discuss what you would like to change.

## License

[MIT](https://choosealicense.com/licenses/mit/)