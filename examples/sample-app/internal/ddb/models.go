package ddb

import "time"

type User struct {
	PK        string    `dynamodbav:"pk"`
	SK        string    `dynamodbav:"sk"`
	UserName  string    `dynamodbav:"user_name"`
	Email     string    `dynamodbav:"email"`
	CreatedAt time.Time `dynamodbav:"created_at"`
}
