package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/luhao/contextGraph/internal/dto"
	"github.com/luhao/contextGraph/internal/model"
	apperr "github.com/luhao/contextGraph/pkg/errors"
)

type UserService interface {
	GetProfileByUserID(userID int64) (*model.User, error)
}

type UserHandler struct {
	userService UserService
}

func NewUserHandler(userService UserService) *UserHandler {
	return &UserHandler{userService: userService}
}

func (h *UserHandler) GetProfile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Unauthorized"))
		return
	}
	user, err := h.userService.GetProfileByUserID(userID.(int64))
	if err != nil {
		// 区分错误类型
		if appErr, ok := apperr.GetAppError(err); ok {
			c.JSON(appErr.Code, dto.Error(appErr.BizCode, appErr.Message))
			return
		}
		c.JSON(http.StatusInternalServerError, dto.Error(apperr.BizUnknown, "Internal server error"))
		return
	}
	profile := dto.UserProfileResponse{
		UserID:   	user.ID,
		Email:    	user.Email,
		Username: 	user.Username,
		Plan:     	user.Plan,
		AvatarURL: 	user.AvatarURL,
		TokenQuota: user.TokenQuota,
	}

	c.JSON(http.StatusOK, dto.Success(profile))
}
