export const MIN_PASSWORD_LENGTH = 8;

export function isPasswordTooShort(password: string): boolean {
  return password.length > 0 && password.length < MIN_PASSWORD_LENGTH;
}
