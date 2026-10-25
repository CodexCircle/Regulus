# Regulus

**CodexCircle Server Manager**, detto anche **Regulus**, è una piattaforma per gestire uno o più server Linux da un'unica dashboard web.

> **Stato del progetto:** in fase di progettazione. Il repository non contiene ancora codice; questo documento descrive l'architettura e lo stack scelti.

## Come funziona

Regulus lavora per **plug-in**. L'installazione base fornisce una dashboard semplice, e ogni plug-in aggiunge funzioni e strumenti.

- **Dashboard**: interfaccia interamente web e personalizzabile. L'utente sceglie quali widget mostrare e come disporli.
- **Agent**: va installato su ogni server da gestire. Collega il server alla rete di server dell'utente.
- **Hub**: va installato su un server per gruppo. Gestisce gli agent a lui collegati e serve la dashboard. Durante l'installazione dell'hub viene chiesto se includere anche l'agent, così il server che ospita l'hub può essere gestito come gli altri.
- **Plug-in**: estendono la piattaforma con nuove funzioni lato server e nuovi widget lato dashboard.

```
  Browser (dashboard React)
            │
            ▼
      ┌───────────┐
      │    Hub    │  un hub per gruppo di server
      └───────────┘
        ▲   ▲   ▲
        │   │   │    connessioni in uscita dagli agent (gRPC, mTLS)
   ┌────┘   │   └────┐
 Agent    Agent    Agent    un agent per ogni server gestito
```

Gli agent aprono la connessione verso l'hub, quindi non servono porte in ingresso sui server gestiti e il sistema funziona anche dietro NAT o firewall.

## Stack tecnologico

| Componente | Tecnologia |
|---|---|
| Agent e hub | [Go](https://go.dev/): un unico eseguibile, usato come agent o come hub a seconda della modalità |
| Dashboard | [React](https://react.dev/) con TypeScript e Vite |
| Comunicazione | gRPC con Protobuf come contratto, mTLS tra agent e hub |
| Plug-in | Processi separati che parlano gRPC con l'host, più moduli JavaScript per i widget della dashboard |
| Dati | SQLite di default, PostgreSQL opzionale |

### Perché questo stack

- **Go** produce un eseguibile statico di poche decine di MB al massimo, senza runtime da installare sui server gestiti, con un consumo di memoria ridotto. È adatto a un agent da installare ovunque.
- **React** ha l'ecosistema più ampio per dashboard a widget e permette di caricare a runtime i moduli dei plug-in.
- **Plug-in out-of-process**: un plug-in che si blocca non abbatte l'agent, i permessi sono isolabili e un plug-in può essere scritto in qualunque linguaggio.

## Obiettivi di progetto

- Agent leggero e installazione in pochi comandi (pacchetti `.deb`/`.rpm` e unità systemd).
- Gestione di più server e più gruppi dalla stessa dashboard.
- Dashboard personalizzabile dall'utente.
- Plug-in installabili senza modificare il nucleo della piattaforma.

## Struttura prevista del repository

Indicativa, da confermare quando inizierà l'implementazione.

```
agent/       codice dell'agent (Go)
hub/         codice dell'hub (Go)
dashboard/   interfaccia web (React + TypeScript)
proto/       definizioni Protobuf condivise
plugins/     plug-in di esempio e documentazione per svilupparne di nuovi
docs/        documentazione
```

## Contribuire

Il progetto è agli inizi. Le decisioni di architettura sono discusse nelle issue e nelle pull request del repository.

## Licenza

Da definire.
