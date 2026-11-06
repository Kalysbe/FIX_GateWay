import { z } from "zod";

// Client-side checks only repeat what the server requires; the server stays the authority.
const filled = (message: string) => z.string().refine(v => v.trim() !== "", message);

export const loginSchema = z.object({
  email: filled("Укажите электронную почту").pipe(z.email("Неверный адрес почты")),
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
