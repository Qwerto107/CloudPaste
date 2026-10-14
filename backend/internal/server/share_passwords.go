package server

// 分享仍用哈希验证；另存加密副本仅供创建者和管理员管理密码。
func (a *App) protectSharePassword(value string) (hash, encrypted string, err error) {
	if value == "" {
		return "", "", nil
	}
	hash, err = passwordHash(value)
	if err != nil {
		return "", "", err
	}
	encrypted, err = a.encrypt(value)
	return
}

func (a *App) addManagedPassword(view map[string]any, hash, encrypted string) error {
	// 旧记录只有不可逆哈希，不能把哈希作为密码展示，也不能自动清除保护。
	view["password_available"] = hash == "" || encrypted != ""
	if hash == "" || encrypted != "" {
		password, err := a.decrypt(encrypted)
		if err != nil {
			return err
		}
		view["password"] = password
	}
	return nil
}

func (a *App) managedPasteView(p Paste, content bool) (map[string]any, error) {
	v := pasteView(p, content)
	return v, a.addManagedPassword(v, p.Password, p.EncryptedPassword)
}
func (a *App) managedFileView(s Share) (map[string]any, error) {
	v := shareView(s)
	return v, a.addManagedPassword(v, s.Password, s.EncryptedPassword)
}
