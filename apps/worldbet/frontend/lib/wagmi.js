import { http, createConfig } from "wagmi";
import { injected } from "wagmi/connectors";
import { seoul, gwangju } from "./chain";

export const wagmiConfig = createConfig({
  chains: [seoul, gwangju],
  connectors: [injected()],
  transports: {
    [seoul.id]: http(),
    [gwangju.id]: http(),
  },
});
