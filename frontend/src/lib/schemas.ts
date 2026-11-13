import { z } from "zod";

// Client-side checks only repeat what the server requires; the server stays the authority.
const filled = (message: string) => z.string().refine(v => v.trim() !== "", message);

export const loginSchema = z.object({
  username: filled("Укажите логин").transform(v => v.trim()),
  password: z.string().min(1, "Введите пароль"),
});
export type LoginValues = z.infer<typeof loginSchema>;

export const brokerCreateSchema = z.object({
  code: filled("Укажите код брокера"),
  name: filled("Укажите название"),
});
export type BrokerCreateValues = z.infer<typeof brokerCreateSchema>;

export const brokerEditSchema = z.object({
  name: filled("Укажите название"),
  description: z.string(),
});
export type BrokerEditValues = z.infer<typeof brokerEditSchema>;

export const ipEntrySchema = z.object({
  ip: filled("Укажите IP-адрес или CIDR"),
  note: z.string(),
});
export type IPEntryValues = z.infer<typeof ipEntrySchema>;

const reasonText = z.string().refine(v => v.trim().length >= 3, "Не короче 3 символов").refine(v => v.length <= 1000, "Не длиннее 1000 символов");

export const tradingLimitSchema = z.object({
  quantity: z.string().refine(v => v.trim() === "" || /^[0-9]+([.][0-9]+)?$/.test(v.trim()), "Число без знака, дробная часть через точку"),
  openOrders: z.string().refine(v => v.trim() === "" || (/^\d+$/.test(v.trim()) && Number(v) >= 1 && Number(v) <= 1_000_000), "Целое число от 1 до 1 000 000"),
  paused: z.boolean(),
  reason: reasonText,
});
export type TradingLimitValues = z.infer<typeof tradingLimitSchema>;

export const quarantineSchema = z.object({
  reason: filled("Опишите причину и план сверки").refine(v => v.length <= 2000, "Не длиннее 2000 символов"),
});
export type QuarantineValues = z.infer<typeof quarantineSchema>;

const limit = z.number({ error: "Укажите число" }).int("Только целое число").min(1, "Не меньше 1").max(1_000_000, "Не больше 1 000 000");
export const ratePolicySchema = z.object({
  scope: z.enum(["broker", "global", "admin"]),
  subject: z.string(),
  operation: z.string().min(1, "Выберите операцию"),
  rate: limit,
  burst: limit,
  concurrent: limit,
  enabled: z.boolean(),
  reason: z.string().refine(v => v.trim().length >= 3, "Не короче 3 символов").refine(v => v.length <= 1000, "Не длиннее 1000 символов"),
}).refine(v => v.scope !== "broker" || v.subject.trim() !== "", { path: ["subject"], message: "Укажите ID брокера" });
export type RatePolicyValues = z.infer<typeof ratePolicySchema>;
