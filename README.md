# srv-identify

Go library for identifying bare-metal servers. Polls IPMI and Redfish to determine vendor, model, and serial number — both remotely (over the network) and locally (from the host itself).

## How it works

`ServerIdentifier` runs one or more **collectors** in sequence and merges their results:

| Collector | Transport        | What it reads                                        |
|-----------|------------------|------------------------------------------------------|
| IPMI      | IPMI over LAN+   | Device ID, FRU (Board/Product/Chassis areas), LAN MAC |
| Redfish   | HTTPS REST API   | Manufacturer, Model, SerialNumber                    |

Vendor name resolution priority: **FRU Product Mfg → FRU Board Mfg → IANA Enterprise ID → OUI (MAC)**. The first non-blank value wins.

## Installation

```bash
go get github.com/realwhite/srv-identify
```

## Usage

### Remote identification

```go
id := identify.NewServerIdentifier(logger)

res, err := id.IdentifyRemote(ctx, net.ParseIP("192.168.1.100"), identify.IdentifyOptions{
    Credentials: identify.ServerCredentials{
        Username: "admin",
        Password: "password",
    },
    IPMIConfig: identify.IPMIConfig{
        Port: 623,
    },
    RedfishConfig: identify.RedfishConfig{
        Port:     443,
        Insecure: true, // skip TLS verification + allow legacy ciphers
    },
})
```

### Local identification

Identifies the server the code is running on. Requires access to `/dev/ipmi0`.

```go
res, err := id.IdentifyLocal(ctx, identify.IdentifyOptions{
    Credentials: identify.ServerCredentials{
        Username: "admin",
        Password: "password",
    },
})
```

### Result

```go
res.Vendor.Name           // "Super Micro Computer, Inc."
res.Vendor.ShortConstant  // "SUPER" — first word, uppercased; useful as a stable key
res.Vendor.IANAEnterpriseID
res.Vendor.MAC
res.Vendor.OUI
res.Vendor.Raw            // all non-blank vendor strings collected across sources

res.Model.Name
res.Model.SerialNumber
```

### Selecting collectors

By default, remote identification uses both IPMI and Redfish; local uses only IPMI.
You can override this:

```go
opts := identify.IdentifyOptions{
    Collectors: []identify.CollectorType{identify.RedfishCollectorType},
    // ...
}
```

## Development

```bash
make test          # run unit tests
make update-oui    # regenerate OUI database from IEEE
make update-iana   # regenerate IANA Enterprise Numbers database
```
