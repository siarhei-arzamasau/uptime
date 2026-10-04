/** An application failure whose status/code/message can be returned in the BFF error envelope. */
export class APIError extends Error {
  constructor(public status: number, public code: string, message: string) { super(message); }
}
